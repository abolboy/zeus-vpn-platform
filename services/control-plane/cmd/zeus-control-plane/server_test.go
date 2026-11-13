package main

import (
	"bytes"
	"context"
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
}
