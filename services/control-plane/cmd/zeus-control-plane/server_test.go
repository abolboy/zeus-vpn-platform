package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeControlStore struct {
	mu       sync.Mutex
	operator Operator
	sessions map[string]Operator
	users    []VPNUser
	audits   []string
	ready    bool
}

func newFakeControlStore(operator Operator) *fakeControlStore {
	return &fakeControlStore{operator: operator, sessions: make(map[string]Operator), ready: true}
}

func (s *fakeControlStore) Ping(context.Context) error {
	if !s.ready {
		return errors.New("not ready")
	}
	return nil
}
func (s *fakeControlStore) FindOperatorByLogin(_ context.Context, login string) (Operator, error) {
	if login != s.operator.Login {
		return Operator{}, pgx.ErrNoRows
	}
	return s.operator, nil
}
func (s *fakeControlStore) EnsureBootstrapOperator(context.Context, string, string) error { return nil }
func (s *fakeControlStore) HasActiveOwner(context.Context) (bool, error) {
	return s.operator.Role == "owner" && s.operator.Status == "active", nil
}
func (s *fakeControlStore) CreateSession(_ context.Context, _ string, hash []byte, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[string(hash)] = Operator{ID: s.operator.ID, Login: s.operator.Login, Role: s.operator.Role, Status: s.operator.Status}
	return nil
}
func (s *fakeControlStore) FindOperatorForSession(_ context.Context, hash []byte) (Operator, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operator, ok := s.sessions[string(hash)]
	if !ok {
		return Operator{}, pgx.ErrNoRows
	}
	return operator, nil
}
func (s *fakeControlStore) RevokeSession(_ context.Context, hash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, string(hash))
	return nil
}
func (s *fakeControlStore) ListUsers(_ context.Context, limit int, cursor *UserCursor) ([]VPNUser, error) {
	if len(s.users) > limit {
		return append([]VPNUser(nil), s.users[:limit]...), nil
	}
	return append([]VPNUser(nil), s.users...), nil
}
func (s *fakeControlStore) RecordAudit(_ context.Context, _ *string, _ *string, action, outcome, _ string, _ *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, action+":"+outcome)
	return nil
}

func newTestAPI(t *testing.T, store ControlStore, origins ...string) http.Handler {
	t.Helper()
	api, err := NewAPI("test", store, nil, origins)
	if err != nil {
		t.Fatalf("NewAPI: %v", err)
	}
	return api.Handler()
}

func TestHealthReadinessAndSecurityHeaders(t *testing.T) {
	handler := newTestAPI(t, nil)
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}
	if health.Header().Get("X-Content-Type-Options") != "nosniff" || health.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("expected security and no-store headers")
	}

	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured readiness status = %d, want 503", ready.Code)
	}
}

func TestPasswordHashAndVerification(t *testing.T) {
	hash, err := hashPassword("a-strong-test-password")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := verifyPassword(hash, "a-strong-test-password")
	if err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	ok, err = verifyPassword(hash, "incorrect-password")
	if err != nil || ok {
		t.Fatalf("incorrect password accepted: ok=%v err=%v", ok, err)
	}
	if _, err := verifyPassword("$argon2id$v=19$m=999999999,t=3,p=2$bad$bad", "x"); err == nil {
		t.Fatal("unbounded hash parameters should be rejected")
	}
}

func TestOperatorLoginMeAndLogout(t *testing.T) {
	passwordHash, err := hashPassword("zeus-test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	store := newFakeControlStore(Operator{
		ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47", Login: "owner",
		Role: "owner", Status: "active", PasswordHash: passwordHash,
	})
	handler := newTestAPI(t, store)

	body := []byte(`{"login":"owner","password":"zeus-test-password-123"}`)
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", login.Code, login.Body.String())
	}
	var session sessionResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.AccessToken == "" || session.TokenType != "Bearer" || session.Operator.Role != "owner" {
		t.Fatalf("unexpected login response: %+v", session)
	}

	meRequest := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	meRequest.Header.Set("Authorization", "Bearer "+session.AccessToken)
	me := httptest.NewRecorder()
	handler.ServeHTTP(me, meRequest)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, body=%s", me.Code, me.Body.String())
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+session.AccessToken)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body=%s", logout.Code, logout.Body.String())
	}

	afterLogout := httptest.NewRecorder()
	handler.ServeHTTP(afterLogout, meRequest)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d, want 401", afterLogout.Code)
	}
	if len(store.audits) != 2 {
		t.Fatalf("audit events = %d, want 2", len(store.audits))
	}
}

func TestLoginDoesNotRevealUnknownAccount(t *testing.T) {
	passwordHash, err := hashPassword("zeus-test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	store := newFakeControlStore(Operator{
		ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47", Login: "owner",
		Role: "owner", Status: "active", PasswordHash: passwordHash,
	})
	handler := newTestAPI(t, store)
	requests := []string{
		`{"login":"owner","password":"wrong-password"}`,
		`{"login":"unknown","password":"wrong-password"}`,
	}
	for _, body := range requests {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewBufferString(body)))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
		if !bytes.Contains(response.Body.Bytes(), []byte("invalid_credentials")) {
			t.Fatalf("unexpected error body: %s", response.Body.String())
		}
	}
}

func TestCORSRequiresExactOrigin(t *testing.T) {
	handler := newTestAPI(t, nil, "https://admin.example")
	allowed := httptest.NewRecorder()
	allowedRequest := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", nil)
	allowedRequest.Header.Set("Origin", "https://admin.example")
	handler.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusNoContent || allowed.Header().Get("Access-Control-Allow-Origin") != "https://admin.example" {
		t.Fatalf("allowed CORS response = %d, %q", allowed.Code, allowed.Header().Get("Access-Control-Allow-Origin"))
	}

	denied := httptest.NewRecorder()
	deniedRequest := httptest.NewRequest(http.MethodGet, "/v1/meta", nil)
	deniedRequest.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(denied, deniedRequest)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unlisted origin status = %d, want 403", denied.Code)
	}
}

func TestPostgresStoreIntegration(t *testing.T) {
	dsn := os.Getenv("ZEUS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZEUS_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := OpenPostgres(ctx, dsn, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	login := "ci-" + newRequestID()
	if err := store.EnsureBootstrapOperator(ctx, login, "ci-owner-password-123"); err != nil {
		t.Fatal(err)
	}
	operator, err := store.FindOperatorByLogin(ctx, login)
	if err != nil {
		t.Fatal(err)
	}
	if operator.Role != "owner" || operator.Status != "active" {
		t.Fatalf("unexpected seeded operator: %+v", operator)
	}
	hasOwner, err := store.HasActiveOwner(ctx)
	if err != nil || !hasOwner {
		t.Fatalf("active owner check failed: hasOwner=%v err=%v", hasOwner, err)
	}
	_, tokenHash, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, operator.ID, tokenHash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindOperatorForSession(ctx, tokenHash); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAudit(ctx, &operator.ID, nil, "ci.integration", "success", newRequestID(), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeSession(ctx, tokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindOperatorForSession(ctx, tokenHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoked session lookup error = %v, want no rows", err)
	}

	// Exercise authenticated user creation, idempotent replay, conflict detection,
	// status update, and readback against the real PostgreSQL service in CI.
	api, err := NewAPI("integration-test", store, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, sessionHash, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, operator.ID, sessionHash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	handler := api.Handler()
	createKey := newRequestID()
	createBody := `{"display_name":"ZEUS Integration User","max_devices":4}`
	createRequest := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/users", bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Idempotency-Key", createKey)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	created := createRequest(createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create user status=%d body=%s", created.Code, created.Body.String())
	}
	var createdUser VPNUser
	if err := json.Unmarshal(created.Body.Bytes(), &createdUser); err != nil {
		t.Fatal(err)
	}
	replayed := createRequest(createBody)
	var replayedUser VPNUser
	if replayed.Code != http.StatusCreated || json.Unmarshal(replayed.Body.Bytes(), &replayedUser) != nil || replayedUser.ID != createdUser.ID {
		t.Fatalf("idempotent replay did not return original user: status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	if replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("expected Idempotency-Replayed response header")
	}
	conflict := createRequest(`{"display_name":"Different Payload","max_devices":4}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status=%d, want 409", conflict.Code)
	}

	updateKey := newRequestID()
	updateBody := `{"status":"suspended","max_devices":5}`
	updateRequest := httptest.NewRequest(http.MethodPatch, "/v1/users/"+createdUser.ID, bytes.NewBufferString(updateBody))
	updateRequest.Header.Set("Authorization", "Bearer "+token)
	updateRequest.Header.Set("Idempotency-Key", updateKey)
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update user status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	var updatedUser VPNUser
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updatedUser); err != nil {
		t.Fatal(err)
	}
	if updatedUser.Status != "suspended" || updatedUser.MaxDevices != 5 {
		t.Fatalf("unexpected updated user: %+v", updatedUser)
	}
	updateReplayRequest := httptest.NewRequest(http.MethodPatch, "/v1/users/"+createdUser.ID, bytes.NewBufferString(updateBody))
	updateReplayRequest.Header.Set("Authorization", "Bearer "+token)
	updateReplayRequest.Header.Set("Idempotency-Key", updateKey)
	updateReplayResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateReplayResponse, updateReplayRequest)
	if updateReplayResponse.Code != http.StatusOK || updateReplayResponse.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("update idempotent replay status=%d body=%s", updateReplayResponse.Code, updateReplayResponse.Body.String())
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/v1/users/"+createdUser.ID, nil)
	getRequest.Header.Set("Authorization", "Bearer "+token)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get user status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	if _, err := store.GetVPNUser(ctx, createdUser.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.pool.Exec(ctx, "UPDATE operators SET role='support' WHERE id=$1::uuid", operator.ID); err != nil {
		t.Fatal(err)
	}
	deniedRequest := httptest.NewRequest(http.MethodPost, "/v1/users", bytes.NewBufferString(`{"display_name":"Forbidden User","max_devices":2}`))
	deniedRequest.Header.Set("Authorization", "Bearer "+token)
	deniedRequest.Header.Set("Idempotency-Key", newRequestID())
	deniedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deniedResponse, deniedRequest)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("support role create status=%d, want 403", deniedResponse.Code)
	}
	if _, err := store.pool.Exec(ctx, "UPDATE operators SET role='owner' WHERE id=$1::uuid", operator.ID); err != nil {
		t.Fatal(err)
	}

	// Verify certificate fingerprint binding, durable command leasing, and idempotent acknowledgements.
	fingerprint := sha256.Sum256([]byte("ZEUS node certificate " + newRequestID()))
	var nodeID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO vpn_nodes (
			name, region, endpoint_host, endpoint_port, public_key, interface_name, status,
			agent_cert_sha256, client_ipv4_pool, client_ipv6_pool, interface_ipv4, interface_ipv6
		)
		VALUES ($1, 'ci', '127.0.0.1', 51820, 'AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=', 'wg0', 'pending',
		        $2, '10.77.77.0/24', 'fd77:77:77::/64', '10.77.77.1', 'fd77:77:77::1')
		RETURNING id::text
	`, "ci-node-"+newRequestID(), fingerprint[:]).Scan(&nodeID)
	if err != nil {
		t.Fatal(err)
	}
	node, err := store.FindNodeByCertFingerprint(ctx, fingerprint[:])
	if err != nil || node.ID != nodeID {
		t.Fatalf("node certificate binding failed: node=%+v err=%v", node, err)
	}
	if _, err := store.FindNodeByCertFingerprint(ctx, []byte("not a fingerprint")); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("invalid node fingerprint error=%v, want ErrNodeNotFound", err)
	}

	commandPayload := []byte("{\"public_key\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\",\"ipv4\":\"10.77.77.3\",\"ipv6\":\"fd77:77:77::3\"}")
	commandHash := sha256.Sum256(commandPayload)
	var commandID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO node_commands (node_id, created_by_operator_id, command_type, payload, request_hash, idempotency_key, expires_at)
		VALUES ($1::uuid, $2::uuid, 'peer.ensure', $3::jsonb, $4, $5, now() + interval '5 minutes')
		RETURNING id::text
	`, nodeID, operator.ID, string(commandPayload), commandHash[:], newRequestID()).Scan(&commandID)
	if err != nil {
		t.Fatal(err)
	}
	command, err := store.LeaseNodeCommand(ctx, nodeID, 45*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if command.ID != commandID || command.NodeID != nodeID || command.Type != "peer.ensure" || command.Attempt != 1 {
		t.Fatalf("unexpected node command lease: %+v", command)
	}
	if _, err := store.LeaseNodeCommand(ctx, nodeID, 45*time.Second); !errors.Is(err, ErrNoNodeCommand) {
		t.Fatalf("second poll error=%v, want ErrNoNodeCommand while lease is active", err)
	}
	if err := store.AcknowledgeNodeCommand(ctx, nodeID, commandID, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AcknowledgeNodeCommand(ctx, nodeID, commandID, "succeeded", ""); err != nil {
		t.Fatalf("replayed acknowledgement should be idempotent: %v", err)
	}
	if err := store.AcknowledgeNodeCommand(ctx, nodeID, commandID, "failed", "wg_apply_failed"); !errors.Is(err, ErrNodeCommandConflict) {
		t.Fatalf("conflicting acknowledgement error=%v, want ErrNodeCommandConflict", err)
	}
	var commandStatus string
	if err := store.pool.QueryRow(ctx, "SELECT status FROM node_commands WHERE id=$1::uuid", commandID).Scan(&commandStatus); err != nil {
		t.Fatal(err)
	}
	if commandStatus != "succeeded" {
		t.Fatalf("node command status=%s, want succeeded", commandStatus)
	}

	var deviceUserID string
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO vpn_users (display_name, max_devices)
		VALUES ('ZEUS Device Integration User', 1)
		RETURNING id::text
	`).Scan(&deviceUserID); err != nil {
		t.Fatal(err)
	}
	deviceInput := CreateDeviceInput{
		DeviceRef: "ci-device-reference-0001",
		DeviceName: "CI Android",
		Platform: "android",
		PublicKey: "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=",
	}
	deviceRoute := "POST:/v1/users/" + deviceUserID + "/devices"
	if _, err := store.pool.Exec(ctx, "UPDATE vpn_nodes SET last_seen_at = now() - interval '3 minutes' WHERE id = $1::uuid", nodeID); err != nil {
		t.Fatal(err)
	}
	staleHash := sha256.Sum256([]byte("stale-node-request"))
	if _, _, err := store.CreateVPNDevice(ctx, operator.ID, deviceUserID, deviceRoute, newRequestID(), staleHash[:], deviceInput, newRequestID(), nil); !errors.Is(err, ErrNoProvisioningNode) {
		t.Fatalf("stale node should not receive a device: err=%v", err)
	}
	if _, err := store.FindNodeByCertFingerprint(ctx, fingerprint[:]); err != nil {
		t.Fatalf("fresh node heartbeat lookup failed: %v", err)
	}
	deviceBody, err := json.Marshal(deviceInput)
	if err != nil {
		t.Fatal(err)
	}
	deviceKey := newRequestID()
	deviceCreateRequest := httptest.NewRequest(http.MethodPost, "/v1/users/"+deviceUserID+"/devices", bytes.NewReader(deviceBody))
	deviceCreateRequest.Header.Set("Authorization", "Bearer "+token)
	deviceCreateRequest.Header.Set("Idempotency-Key", deviceKey)
	deviceCreateResponse := httptest.NewRecorder()
	handler.ServeHTTP(deviceCreateResponse, deviceCreateRequest)
	if deviceCreateResponse.Code != http.StatusAccepted {
		t.Fatalf("device create API status=%d body=%s", deviceCreateResponse.Code, deviceCreateResponse.Body.String())
	}
	var device VPNDevice
	if err := json.Unmarshal(deviceCreateResponse.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	if device.Status != "provisioning" || device.Peer.Status != "pending" ||
		device.Peer.AssignedIPv4 != "10.77.77.2" || device.Peer.AssignedIPv6 != "fd77:77:77::2" ||
		device.Peer.ServerPublicKey != "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=" {
		t.Fatalf("unexpected device provisioning result: %+v", device)
	}
	deviceReplayRequest := httptest.NewRequest(http.MethodPost, "/v1/users/"+deviceUserID+"/devices", bytes.NewReader(deviceBody))
	deviceReplayRequest.Header.Set("Authorization", "Bearer "+token)
	deviceReplayRequest.Header.Set("Idempotency-Key", deviceKey)
	deviceReplayResponse := httptest.NewRecorder()
	handler.ServeHTTP(deviceReplayResponse, deviceReplayRequest)
	var replayedDevice VPNDevice
	if deviceReplayResponse.Code != http.StatusAccepted ||
		deviceReplayResponse.Header().Get("Idempotency-Replayed") != "true" ||
		json.Unmarshal(deviceReplayResponse.Body.Bytes(), &replayedDevice) != nil ||
		replayedDevice.ID != device.ID {
		t.Fatalf("device create replay failed: status=%d body=%s", deviceReplayResponse.Code, deviceReplayResponse.Body.String())
	}
	devices, err := store.ListVPNDevices(ctx, deviceUserID)
	if err != nil || len(devices) != 1 || devices[0].ID != device.ID {
		t.Fatalf("device list mismatch: devices=%+v err=%v", devices, err)
	}
	deviceListRequest := httptest.NewRequest(http.MethodGet, "/v1/users/"+deviceUserID+"/devices", nil)
	deviceListRequest.Header.Set("Authorization", "Bearer "+token)
	deviceListResponse := httptest.NewRecorder()
	handler.ServeHTTP(deviceListResponse, deviceListRequest)
	if deviceListResponse.Code != http.StatusOK {
		t.Fatalf("device list API status=%d body=%s", deviceListResponse.Code, deviceListResponse.Body.String())
	}

	secondDeviceBody, err := json.Marshal(CreateDeviceInput{
		DeviceRef: "ci-device-reference-0002", DeviceName: "Second Android",
		Platform: "android", PublicKey: "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondDeviceRequest := httptest.NewRequest(http.MethodPost, "/v1/users/"+deviceUserID+"/devices", bytes.NewReader(secondDeviceBody))
	secondDeviceRequest.Header.Set("Authorization", "Bearer "+token)
	secondDeviceRequest.Header.Set("Idempotency-Key", newRequestID())
	secondDeviceResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondDeviceResponse, secondDeviceRequest)
	if secondDeviceResponse.Code != http.StatusConflict {
		t.Fatalf("device limit API status=%d body=%s, want 409", secondDeviceResponse.Code, secondDeviceResponse.Body.String())
	}

	secondHash := sha256.Sum256([]byte("second-device"))
	_, _, err = store.CreateVPNDevice(ctx, operator.ID, deviceUserID, deviceRoute, newRequestID(),
		secondHash[:], CreateDeviceInput{
			DeviceRef: "ci-device-reference-0002", DeviceName: "Second Android",
			Platform: "android", PublicKey: deviceInput.PublicKey,
		}, newRequestID(), nil)
	if !errors.Is(err, ErrDeviceLimitReached) {
		t.Fatalf("second device error=%v, want ErrDeviceLimitReached", err)
	}

	deviceCommand, err := store.LeaseNodeCommand(ctx, nodeID, 45*time.Second)
	if err != nil || deviceCommand.Type != "peer.ensure" {
		t.Fatalf("device peer.ensure command lease failed: command=%+v err=%v", deviceCommand, err)
	}
	if err := store.AcknowledgeNodeCommand(ctx, nodeID, deviceCommand.ID, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	devices, err = store.ListVPNDevices(ctx, deviceUserID)
	if err != nil || len(devices) != 1 || devices[0].Status != "active" || devices[0].Peer.Status != "active" {
		t.Fatalf("peer ensure acknowledgement did not activate device: devices=%+v err=%v", devices, err)
	}

	revokeRoute := "POST:/v1/users/" + deviceUserID + "/devices/" + device.ID + "/revoke"
	revokeKey := newRequestID()
	revokeHash := sha256.Sum256([]byte("zeus-device-revoke-v1"))
	revoking, revokeReplay, err := store.RevokeVPNDevice(ctx, operator.ID, deviceUserID, device.ID, revokeRoute, revokeKey, revokeHash[:], newRequestID(), nil)
	if err != nil || revokeReplay || revoking.Status != "revoking" || revoking.Peer.Status != "revoking" {
		t.Fatalf("device revoke enqueue failed: replay=%v device=%+v err=%v", revokeReplay, revoking, err)
	}
	revokeAgain, revokeReplay, err := store.RevokeVPNDevice(ctx, operator.ID, deviceUserID, device.ID, revokeRoute, revokeKey, revokeHash[:], newRequestID(), nil)
	if err != nil || !revokeReplay || revokeAgain.ID != device.ID {
		t.Fatalf("device revoke replay failed: replay=%v device=%+v err=%v", revokeReplay, revokeAgain, err)
	}

	revokeAPIRequest := httptest.NewRequest(http.MethodPost, "/v1/users/"+deviceUserID+"/devices/"+device.ID+"/revoke", nil)
	revokeAPIRequest.Header.Set("Authorization", "Bearer "+token)
	revokeAPIRequest.Header.Set("Idempotency-Key", newRequestID())
	revokeAPIResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokeAPIResponse, revokeAPIRequest)
	if revokeAPIResponse.Code != http.StatusAccepted {
		t.Fatalf("device revoke API status=%d body=%s", revokeAPIResponse.Code, revokeAPIResponse.Body.String())
	}
	removeCommand, err := store.LeaseNodeCommand(ctx, nodeID, 45*time.Second)
	if err != nil || removeCommand.Type != "peer.remove" {
		t.Fatalf("device peer.remove command lease failed: command=%+v err=%v", removeCommand, err)
	}
	if err := store.AcknowledgeNodeCommand(ctx, nodeID, removeCommand.ID, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	devices, err = store.ListVPNDevices(ctx, deviceUserID)
	if err != nil || len(devices) != 1 || devices[0].Status != "revoked" || devices[0].Peer.Status != "revoked" {
		t.Fatalf("peer remove acknowledgement did not revoke device: devices=%+v err=%v", devices, err)
	}

	// A successfully revoked peer releases its per-node IPv4/IPv6 addresses for reuse.
	replacementInput := CreateDeviceInput{
		DeviceRef: "ci-device-reference-0003",
		DeviceName: "Replacement Android",
		Platform: "android",
		PublicKey: "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ=",
	}
	replacementHash := sha256.Sum256([]byte("replacement-device-payload"))
	replacementDevice, replacementWasReplay, replacementErr := store.CreateVPNDevice(
		ctx, operator.ID, deviceUserID, deviceRoute, newRequestID(), replacementHash[:],
		replacementInput, newRequestID(), nil,
	)
	if replacementErr != nil || replacementWasReplay {
		t.Fatalf("replacement device create failed: replay=%v device=%+v err=%v", replacementWasReplay, replacementDevice, replacementErr)
	}
	if replacementDevice.Peer.AssignedIPv4 != device.Peer.AssignedIPv4 ||
		replacementDevice.Peer.AssignedIPv6 != device.Peer.AssignedIPv6 {
		t.Fatalf("released node addresses were not reused: old=%+v new=%+v", device.Peer, replacementDevice.Peer)
	}

	// Issue a one-time enrollment code from the authenticated admin API.
	var enrollmentUserID string
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO vpn_users (display_name, max_devices)
		VALUES ('ZEUS Client Enrollment User', 1)
		RETURNING id::text
	`).Scan(&enrollmentUserID); err != nil {
		t.Fatal(err)
	}
	issueCodeRequest := httptest.NewRequest(http.MethodPost, "/v1/users/"+enrollmentUserID+"/enrollment-code", nil)
	issueCodeRequest.Header.Set("Authorization", "Bearer "+token)
	issueCodeResponse := httptest.NewRecorder()
	handler.ServeHTTP(issueCodeResponse, issueCodeRequest)
	if issueCodeResponse.Code != http.StatusCreated {
		t.Fatalf("enrollment code issue status=%d body=%s", issueCodeResponse.Code, issueCodeResponse.Body.String())
	}
	var enrollmentCode EnrollmentCodeResponse
	if err := json.Unmarshal(issueCodeResponse.Body.Bytes(), &enrollmentCode); err != nil {
		t.Fatal(err)
	}
	if len(enrollmentCode.Code) != 43 || !enrollmentCode.ExpiresAt.After(time.Now()) {
		t.Fatalf("invalid one-time enrollment response: %+v", enrollmentCode)
	}

	enrollmentInput := ClientEnrollmentInput{
		ActivationCode: enrollmentCode.Code,
		DeviceRef: "ci-enrollment-device-0001",
		DeviceName: "Enrollment Android",
		Platform: "android",
		PublicKey: "AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=",
	}
	enrollmentBody, err := json.Marshal(enrollmentInput)
	if err != nil {
		t.Fatal(err)
	}
	enrollmentKey := newRequestID()
	clientEnrollRequest := httptest.NewRequest(http.MethodPost, "/v1/client/enroll", bytes.NewReader(enrollmentBody))
	clientEnrollRequest.Header.Set("Idempotency-Key", enrollmentKey)
	clientEnrollResponse := httptest.NewRecorder()
	handler.ServeHTTP(clientEnrollResponse, clientEnrollRequest)
	if clientEnrollResponse.Code != http.StatusAccepted {
		t.Fatalf("client enrollment status=%d body=%s", clientEnrollResponse.Code, clientEnrollResponse.Body.String())
	}
	var enrolledDevice VPNDevice
	if err := json.Unmarshal(clientEnrollResponse.Body.Bytes(), &enrolledDevice); err != nil {
		t.Fatal(err)
	}
	if enrolledDevice.UserID != enrollmentUserID || enrolledDevice.Status != "provisioning" ||
		enrolledDevice.Peer.Status != "pending" || enrolledDevice.Peer.PublicKey != enrollmentInput.PublicKey {
		t.Fatalf("unexpected client enrollment result: %+v", enrolledDevice)
	}

	// Lost-response retry with the same key is idempotent.
	clientEnrollReplay := httptest.NewRequest(http.MethodPost, "/v1/client/enroll", bytes.NewReader(enrollmentBody))
	clientEnrollReplay.Header.Set("Idempotency-Key", enrollmentKey)
	clientEnrollReplayResponse := httptest.NewRecorder()
	handler.ServeHTTP(clientEnrollReplayResponse, clientEnrollReplay)
	var enrolledReplay VPNDevice
	if clientEnrollReplayResponse.Code != http.StatusAccepted ||
		clientEnrollReplayResponse.Header().Get("Idempotency-Replayed") != "true" ||
		json.Unmarshal(clientEnrollReplayResponse.Body.Bytes(), &enrolledReplay) != nil ||
		enrolledReplay.ID != enrolledDevice.ID {
		t.Fatalf("client enrollment idempotent replay failed: status=%d body=%s", clientEnrollReplayResponse.Code, clientEnrollReplayResponse.Body.String())
	}

	// A retry with a fresh idempotency key but identical client identity recovers the same device.
	clientEnrollFreshRetry := httptest.NewRequest(http.MethodPost, "/v1/client/enroll", bytes.NewReader(enrollmentBody))
	clientEnrollFreshRetry.Header.Set("Idempotency-Key", newRequestID())
	clientEnrollFreshRetryResponse := httptest.NewRecorder()
	handler.ServeHTTP(clientEnrollFreshRetryResponse, clientEnrollFreshRetry)
	var enrolledFreshRetry VPNDevice
	if clientEnrollFreshRetryResponse.Code != http.StatusAccepted ||
		json.Unmarshal(clientEnrollFreshRetryResponse.Body.Bytes(), &enrolledFreshRetry) != nil ||
		enrolledFreshRetry.ID != enrolledDevice.ID {
		t.Fatalf("fresh-key enrollment recovery failed: status=%d body=%s", clientEnrollFreshRetryResponse.Code, clientEnrollFreshRetryResponse.Body.String())
	}

	// The same one-time code cannot provision another key or another device reference.
	secondEnrollmentInput := enrollmentInput
	secondEnrollmentInput.DeviceRef = "ci-enrollment-device-0002"
	secondEnrollmentInput.PublicKey = "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ="
	secondEnrollmentBody, err := json.Marshal(secondEnrollmentInput)
	if err != nil {
		t.Fatal(err)
	}
	secondClientEnroll := httptest.NewRequest(http.MethodPost, "/v1/client/enroll", bytes.NewReader(secondEnrollmentBody))
	secondClientEnroll.Header.Set("Idempotency-Key", newRequestID())
	secondClientEnrollResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondClientEnrollResponse, secondClientEnroll)
	if secondClientEnrollResponse.Code != http.StatusConflict ||
		!bytes.Contains(secondClientEnrollResponse.Body.Bytes(), []byte("activation_code_used")) {
		t.Fatalf("consumed activation code status=%d body=%s, want 409 activation_code_used", secondClientEnrollResponse.Code, secondClientEnrollResponse.Body.String())
	}

}
