package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"errors"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type nodeAgentE2ECertFiles struct {
	caPath     string
	serverCert string
	serverKey  string
	clientCert string
	clientKey  string
	clientPEM  []byte
}

type synchronizedE2EBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedE2EBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *synchronizedE2EBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type nodeAgentProcess struct {
	cmd        *exec.Cmd
	done       chan struct{}
	waitErr    error
	output     *synchronizedE2EBuffer
	once       sync.Once
	privileged bool
	stopErr    error
}

func TestNodeAgentProcessEndToEnd(t *testing.T) {
	dsn := os.Getenv("ZEUS_E2E_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZEUS_E2E_TEST_DATABASE_URL is not configured; isolated node-agent E2E is opt-in")
	}
	if os.Getenv("ZEUS_E2E_DATABASE_IS_DISPOSABLE") != "true" {
		t.Fatal("refusing to run: ZEUS_E2E_DATABASE_IS_DISPOSABLE=true is required")
	}
	if err := validateE2ETestDSN(dsn); err != nil {
		t.Fatalf("refusing non-disposable E2E database: %v", err)
	}
	if err := requireE2ERootRunner(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store, err := OpenPostgres(ctx, dsn, false)
	if err != nil {
		t.Fatalf("open disposable E2E PostgreSQL: %v", err)
	}
	t.Cleanup(store.Close)
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("E2E database schema is not ready: %v", err)
	}

	api, err := NewAPI("node-agent-e2e", store, nil, nil)
	if err != nil {
		t.Fatalf("initialize control-plane API: %v", err)
	}
	handler := api.Handler()

	operatorLogin := "e2e-" + strings.ReplaceAll(newRequestID(), "-", "")
	operatorPasswordHash, err := hashPassword("CI-only-e2e-password-123456")
	if err != nil {
		t.Fatalf("hash isolated E2E operator password: %v", err)
	}
	var operator Operator
	err = store.pool.QueryRow(ctx, `
		INSERT INTO operators (login, password_hash, role, status)
		VALUES ($1, $2, 'admin', 'active')
		RETURNING id::text, login, role, status
	`, operatorLogin, operatorPasswordHash).Scan(&operator.ID, &operator.Login, &operator.Role, &operator.Status)
	if err != nil {
		t.Fatalf("create isolated E2E operator fixture: %v", err)
	}
	token, tokenHash, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate E2E operator session: %v", err)
	}
	if err := store.CreateSession(ctx, operator.ID, tokenHash, time.Now().Add(10*time.Minute)); err != nil {
		t.Fatalf("persist E2E operator session: %v", err)
	}

	nodeInput := CreateVPNNodeInput{
		Name:           "ZEUS E2E " + strings.ReplaceAll(newRequestID(), "-", ""),
		Region:         "CI",
		EndpointHost:   "127.0.0.1",
		EndpointPort:   51820,
		PublicKey:      randomWireGuardKey(t),
		InterfaceName:  "wg0",
		ClientIPv4Pool: "10.88.77.0/24",
		ClientIPv6Pool: "fd88:77:77::/64",
		InterfaceIPv4:  "10.88.77.1",
		InterfaceIPv6:  "fd88:77:77::1",
	}
	nodeBody, err := json.Marshal(nodeInput)
	if err != nil {
		t.Fatal(err)
	}
	nodeResponse := serveE2EJSON(t, handler, http.MethodPost, "/v1/nodes", nodeBody, token, newRequestID())
	if nodeResponse.Code != http.StatusCreated {
		t.Fatalf("create E2E node: status=%d body=%s", nodeResponse.Code, nodeResponse.Body.String())
	}
	var node VPNNode
	if err := json.Unmarshal(nodeResponse.Body.Bytes(), &node); err != nil {
		t.Fatalf("decode created E2E node: %v", err)
	}
	if node.ID == "" || node.Status != "pending" {
		t.Fatalf("unexpected initial E2E node state: %+v", node)
	}

	var e2eUserID string
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := cleanupNodeAgentE2ERecords(cleanupCtx, store, node.ID, e2eUserID, operator.ID); err != nil {
			t.Errorf("clean up isolated Node Agent E2E records: %v", err)
		}
	})

	certificates := createNodeAgentE2ECertificates(t, node.ID)
	caPEM, err := os.ReadFile(certificates.caPath)
	if err != nil {
		t.Fatal(err)
	}
	nodeRoots := x509.NewCertPool()
	if !nodeRoots.AppendCertsFromPEM(caPEM) {
		t.Fatal("could not load generated E2E node CA")
	}
	api.SetNodeClientCAs(nodeRoots)

	registerBody, err := json.Marshal(RegisterNodeCertificateInput{CertificatePEM: string(certificates.clientPEM)})
	if err != nil {
		t.Fatal(err)
	}
	registerResponse := serveE2EJSON(
		t, handler, http.MethodPost,
		"/v1/nodes/"+node.ID+"/certificates",
		registerBody, token, "",
	)
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("register E2E node certificate: status=%d body=%s", registerResponse.Code, registerResponse.Body.String())
	}

	serverTLS, err := loadNodeTLSConfig(certificates.serverCert, certificates.serverKey, certificates.caPath)
	if err != nil {
		t.Fatalf("load isolated mTLS listener certificates: %v", err)
	}
	nodeServer := httptest.NewUnstartedServer(NewNodeAgentHandler(store, nil))
	nodeServer.TLS = serverTLS
	nodeServer.StartTLS()
	t.Cleanup(nodeServer.Close)

	repoRoot, err := e2ERepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	agentBinary := filepath.Join(t.TempDir(), "zeus-vpn-node")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", agentBinary, "./cmd/zeus-vpn-node")
	build.Dir = filepath.Join(repoRoot, "services", "vpn-node")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real VPN node-agent binary: %v\n%s", err, output)
	}

	wireguardDir := t.TempDir()
	configPath := filepath.Join(wireguardDir, "wg0.conf")
	config := "[Interface]\nPrivateKey = " + randomWireGuardKey(t) +
		"\nAddress = 10.88.77.1/24, fd88:77:77::1/64\nListenPort = 51820\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write isolated fake WireGuard config: %v", err)
	}

	wgLog := filepath.Join(wireguardDir, "wg.log")
	wgBinary := filepath.Join(wireguardDir, "wg-fake")
	wgScript := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(wgLog) + "\nexit 0\n"
	if err := os.WriteFile(wgBinary, []byte(wgScript), 0o700); err != nil {
		t.Fatalf("write fake wg executable: %v", err)
	}
	ipBinary := filepath.Join(wireguardDir, "ip-fake")
	ipScript := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  \"-4 route show exact \"*|\"-6 route show exact \"*) exit 0 ;;\n" +
		"  \"-4 route get \"*|\"-6 route get \"*) printf '%s\\n' 'dev wg0'; exit 0 ;;\n" +
		"  *) exit 0 ;;\n" +
		"esac\n"
	if err := os.WriteFile(ipBinary, []byte(ipScript), 0o700); err != nil {
		t.Fatalf("write fake ip executable: %v", err)
	}

	agent, err := startNodeAgentForE2E(t, agentBinary, map[string]string{
		"ZEUS_CONTROL_PLANE_URL":     nodeServer.URL,
		"ZEUS_NODE_CLIENT_CERT_FILE": certificates.clientCert,
		"ZEUS_NODE_CLIENT_KEY_FILE":  certificates.clientKey,
		"ZEUS_NODE_SERVER_CA_FILE":   certificates.caPath,
		"ZEUS_NODE_ALLOW_MUTATIONS":  "true",
		"ZEUS_NODE_POLL_INTERVAL":    "1s",
		"ZEUS_WG_INTERFACE":          "wg0",
		"ZEUS_WG_CONFIG_PATH":        configPath,
		"ZEUS_WG_BINARY":             wgBinary,
		"ZEUS_IP_BINARY":             ipBinary,
		"ZEUS_WG_IPV4_POOL":          nodeInput.ClientIPv4Pool,
		"ZEUS_WG_IPV6_POOL":          nodeInput.ClientIPv6Pool,
	})
	if err != nil {
		t.Fatalf("start real node-agent process: %v", err)
	}
	t.Cleanup(func() {
		if err := agent.stop(); err != nil {
			t.Errorf("stop Node Agent E2E process: %v", err)
		}
	})

	waitForE2ECondition(t, ctx, agent, "node to authenticate over mTLS and become online", func() (bool, error) {
		var status string
		err := store.pool.QueryRow(ctx, "SELECT status FROM vpn_nodes WHERE id=$1::uuid", node.ID).Scan(&status)
		return status == "online", err
	})

	userInput := CreateUserInput{
		DisplayName: "ZEUS E2E Customer " + strings.ReplaceAll(newRequestID(), "-", "")[:12],
		MaxDevices:  2,
	}
	userBody, err := json.Marshal(userInput)
	if err != nil {
		t.Fatal(err)
	}
	userResponse := serveE2EJSON(t, handler, http.MethodPost, "/v1/users", userBody, token, newRequestID())
	if userResponse.Code != http.StatusCreated {
		t.Fatalf("create E2E customer: status=%d body=%s", userResponse.Code, userResponse.Body.String())
	}
	var user VPNUser
	if err := json.Unmarshal(userResponse.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode E2E customer: %v", err)
	}
	if user.Status != "active" || user.ID == "" {
		t.Fatalf("unexpected E2E customer: %+v", user)
	}
	e2eUserID = user.ID

	codeResponse := serveE2EJSON(
		t, handler, http.MethodPost,
		"/v1/users/"+user.ID+"/enrollment-code",
		[]byte("{}"), token, "",
	)
	if codeResponse.Code != http.StatusCreated {
		t.Fatalf("issue customer activation code: status=%d body=%s", codeResponse.Code, codeResponse.Body.String())
	}
	var enrollment EnrollmentCodeResponse
	if err := json.Unmarshal(codeResponse.Body.Bytes(), &enrollment); err != nil {
		t.Fatalf("decode customer activation code response: %v", err)
	}
	if len(enrollment.Code) != 43 || !enrollment.ExpiresAt.After(time.Now()) {
		t.Fatalf("invalid customer activation code response: %+v", enrollment)
	}

	clientPublicKey := randomWireGuardKey(t)
	clientEnrollment := ClientEnrollmentInput{
		ActivationCode: enrollment.Code,
		DeviceRef:      "ci-e2e-" + strings.ReplaceAll(newRequestID(), "-", ""),
		DeviceName:     "ZEUS CI Android",
		Platform:       "android",
		PublicKey:      clientPublicKey,
	}
	enrollmentBody, err := json.Marshal(clientEnrollment)
	if err != nil {
		t.Fatal(err)
	}
	enrollResponse := serveE2EJSON(
		t, handler, http.MethodPost,
		"/v1/client/enroll",
		enrollmentBody, "", newRequestID(),
	)
	if enrollResponse.Code != http.StatusAccepted {
		t.Fatalf("customer device enrollment: status=%d body=%s", enrollResponse.Code, enrollResponse.Body.String())
	}
	var device VPNDevice
	if err := json.Unmarshal(enrollResponse.Body.Bytes(), &device); err != nil {
		t.Fatalf("decode customer device enrollment: %v", err)
	}
	if device.Status != "provisioning" || device.Peer.Status != "pending" || device.Peer.NodeID != node.ID {
		t.Fatalf("unexpected initial provisioned device: %+v", device)
	}

	var activeDevice VPNDevice
	waitForE2ECondition(t, ctx, agent, "real node-agent execution and successful command acknowledgement", func() (bool, error) {
		current, err := store.GetCustomerVPNDevice(ctx, user.ID, device.ID)
		if err != nil {
			return false, err
		}
		activeDevice = current
		return current.Status == "active" && current.Peer.Status == "active", nil
	})

	var commandStatus string
	var attempts int
	query := "SELECT status, attempts FROM node_commands " +
		"WHERE node_id=$1::uuid AND payload->>'public_key'=$2 " +
		"ORDER BY created_at DESC, id DESC LIMIT 1"
	if err := store.pool.QueryRow(ctx, query, node.ID, clientPublicKey).Scan(&commandStatus, &attempts); err != nil {
		t.Fatalf("read completed peer.ensure command: %v", err)
	}
	if commandStatus != "succeeded" || attempts != 1 {
		t.Fatalf("unexpected peer.ensure state: status=%s attempts=%d", commandStatus, attempts)
	}
	wgOutput, err := os.ReadFile(wgLog)
	if err != nil {
		t.Fatalf("read fake WireGuard execution log: %v", err)
	}
	if !bytes.Contains(wgOutput, []byte("set wg0 peer "+clientPublicKey+" allowed-ips ")) {
		t.Fatalf("real node-agent did not invoke the isolated fake wg executor; log=%s", wgOutput)
	}
	if activeDevice.Peer.AssignedIPv4 == "" || activeDevice.Peer.AssignedIPv6 == "" {
		t.Fatalf("active device has no allocated client addresses: %+v", activeDevice.Peer)
	}
}

func cleanupNodeAgentE2ERecords(ctx context.Context, store *PostgresStore, nodeID, userID, operatorID string) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if userID != "" {
		if _, err := tx.Exec(ctx, "DELETE FROM vpn_user_enrollment_codes WHERE user_id=$1::uuid", userID); err != nil {
			return fmt.Errorf("delete test enrollment codes: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM wireguard_peers WHERE node_id=$1::uuid", nodeID); err != nil {
		return fmt.Errorf("delete test peers: %w", err)
	}
	if userID != "" {
		if _, err := tx.Exec(ctx, "DELETE FROM vpn_devices WHERE user_id=$1::uuid", userID); err != nil {
			return fmt.Errorf("delete test devices: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM node_commands WHERE node_id=$1::uuid", nodeID); err != nil {
		return fmt.Errorf("delete test node commands: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM vpn_node_agent_certificates WHERE node_id=$1::uuid", nodeID); err != nil {
		return fmt.Errorf("delete test node certificates: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM vpn_nodes WHERE id=$1::uuid", nodeID); err != nil {
		return fmt.Errorf("delete test node: %w", err)
	}
	if userID != "" {
		if _, err := tx.Exec(ctx, "DELETE FROM vpn_users WHERE id=$1::uuid", userID); err != nil {
			return fmt.Errorf("delete test customer: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM operator_sessions WHERE operator_id=$1::uuid", operatorID); err != nil {
		return fmt.Errorf("delete test operator sessions: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM operators WHERE id=$1::uuid", operatorID); err != nil {
		return fmt.Errorf("delete test operator: %w", err)
	}
	return tx.Commit(ctx)
}

func TestValidateE2ETestDSN(t *testing.T) {
	const validDSN = "postgres://zeus_test:zeus_ci_only_password@127.0.0.1:5432/zeus_test?sslmode=disable"
	if err := validateE2ETestDSN(validDSN); err != nil {
		t.Fatalf("current CI DSN must be accepted: %v", err)
	}

	tests := []struct {
		name string
		dsn  string
	}{
		{
			name: "query host override",
			dsn:  validDSN + "&host=10.0.0.10",
		},
		{
			name: "query user override",
			dsn:  validDSN + "&user=app",
		},
		{
			name: "query database override",
			dsn:  validDSN + "&database=other",
		},
		{
			name: "query port override",
			dsn:  validDSN + "&port=5433",
		},
		{
			name: "multiple hosts",
			dsn:  "postgres://zeus_test:zeus_ci_only_password@127.0.0.1:5432,10.0.0.10:5432/zeus_test?sslmode=disable",
		},
		{
			name: "TLS mode override",
			dsn:  validDSN + "&sslmode=prefer",
		},
		{
			name: "duplicate TLS mode",
			dsn:  validDSN + "&sslmode=verify-full",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateE2ETestDSN(test.dsn); err == nil {
				t.Fatal("expected unsafe database DSN to be rejected")
			}
		})
	}
}

func validateE2ETestDSN(dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid PostgreSQL URL")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return fmt.Errorf("expected PostgreSQL URL")
	}

	// Keep the E2E URL deliberately narrow: CI uses only sslmode=disable.
	// This rejects duplicate and query-string overrides before pgx can merge them.
	query := parsed.Query()
	sslModes := query["sslmode"]
	if len(query) != 1 || len(sslModes) != 1 || sslModes[0] != "disable" {
		return fmt.Errorf("E2E database URL may contain only one sslmode=disable parameter")
	}

	// Use the exact parser used by OpenPostgres so validation sees pgx's effective
	// settings, not only the URL authority (which query parameters can override).
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid PostgreSQL connection configuration")
	}
	connection := config.ConnConfig
	if connection.Host != "127.0.0.1" {
		return fmt.Errorf("effective database host must be 127.0.0.1")
	}
	if connection.Port != 5432 {
		return fmt.Errorf("effective database port must be 5432")
	}
	if connection.Database != "zeus_test" {
		return fmt.Errorf("effective database name must be zeus_test")
	}
	if connection.User != "zeus_test" {
		return fmt.Errorf("effective database role must be zeus_test")
	}
	if connection.Password == "" {
		return fmt.Errorf("E2E database password must be explicit in the CI URL")
	}
	if connection.TLSConfig != nil || len(connection.Fallbacks) != 0 || connection.SSLNegotiation != "" {
		return fmt.Errorf("E2E database connection must use TLS-disabled loopback with no fallback targets")
	}
	return nil
}

func requireE2ERootRunner() error {
	if os.Geteuid() == 0 {
		return nil
	}
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo is unavailable; root execution is required for the isolated fake-mutation path")
	}
	if output, err := exec.Command(sudo, "-n", "true").CombinedOutput(); err != nil {
		return fmt.Errorf("non-interactive sudo is required for the isolated fake-mutation test: %v %s", err, output)
	}
	return nil
}

func serveE2EJSON(
	t *testing.T,
	handler http.Handler,
	method, path string,
	body []byte,
	bearerToken, idempotencyKey string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func randomWireGuardKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate test WireGuard key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func e2ERepositoryRoot() (string, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("could not locate E2E test source file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../")), nil
}

func createNodeAgentE2ECertificates(t *testing.T, nodeID string) nodeAgentE2ECertFiles {
	t.Helper()
	directory := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "ZEUS CI E2E CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caPath := writeNodeAgentE2EFile(t, directory, "ca.pem", caPEM, 0o600)

	serverCertificate, serverKey := issueNodeAgentE2ELeaf(
		t, caCertificate, caKey, now, "ZEUS CI E2E mTLS server",
		x509.ExtKeyUsageServerAuth, nil, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")},
	)
	clientURI, err := url.Parse("spiffe://zeus/node/" + nodeID)
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, clientKey := issueNodeAgentE2ELeaf(
		t, caCertificate, caKey, now, "ZEUS CI E2E node client",
		x509.ExtKeyUsageClientAuth, []*url.URL{clientURI}, nil, nil,
	)

	return nodeAgentE2ECertFiles{
		caPath:     caPath,
		serverCert: writeNodeAgentE2EFile(t, directory, "server.crt", serverCertificate, 0o644),
		serverKey:  writeNodeAgentE2EFile(t, directory, "server.key", serverKey, 0o600),
		clientCert: writeNodeAgentE2EFile(t, directory, "client.crt", clientCertificate, 0o644),
		clientKey:  writeNodeAgentE2EFile(t, directory, "client.key", clientKey, 0o600),
		clientPEM:  clientCertificate,
	}
}

func issueNodeAgentE2ELeaf(
	t *testing.T,
	ca *x509.Certificate,
	caKey *ecdsa.PrivateKey,
	now time.Time,
	commonName string,
	usage x509.ExtKeyUsage,
	uris []*url.URL,
	dnsNames []string,
	ipAddresses []net.IP,
) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano() + int64(len(commonName))),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(12 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		URIs:         uris,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	return certificatePEM, keyPEM
}

func writeNodeAgentE2EFile(t *testing.T, directory, name string, content []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write E2E certificate file %s: %v", name, err)
	}
	return path
}

func startNodeAgentForE2E(t *testing.T, binary string, environment map[string]string) (*nodeAgentProcess, error) {
	t.Helper()
	output := &synchronizedE2EBuffer{}
	privileged := os.Geteuid() != 0
	envNames := make([]string, 0, len(environment))
	for name := range environment {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)
	envPairs := make([]string, 0, len(envNames))
	for _, name := range envNames {
		envPairs = append(envPairs, name+"="+environment[name])
	}

	var command *exec.Cmd
	if os.Geteuid() == 0 {
		command = exec.Command(binary)
		command.Env = append(os.Environ(), envPairs...)
	} else {
		sudo, err := exec.LookPath("sudo")
		if err != nil {
			return nil, fmt.Errorf("sudo is required to test the explicit mutation path: %w", err)
		}
		args := []string{"-n", "--", "/usr/bin/env"}
		args = append(args, envPairs...)
		args = append(args, binary)
		command = exec.Command(sudo, args...)
	}
	command.Stdout = output
	command.Stderr = output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("launch isolated node-agent process: %w", err)
	}
	process := &nodeAgentProcess{
		cmd:        command,
		done:       make(chan struct{}),
		output:     output,
		privileged: privileged,
	}
	go func() {
		process.waitErr = command.Wait()
		close(process.done)
	}()
	return process, nil
}

func (p *nodeAgentProcess) signalGroup(signal syscall.Signal) error {
	pid := p.cmd.Process.Pid
	if err := syscall.Kill(-pid, signal); err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	} else if !errors.Is(err, syscall.EPERM) || !p.privileged {
		return fmt.Errorf("signal process group with signal %d: %w", signal, err)
	}

	// A non-root test process cannot signal the root-owned agent started through
	// sudo. Use the same non-interactive privilege path for bounded cleanup.
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("direct signal failed and sudo is unavailable: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		sudo,
		"-n",
		"kill",
		fmt.Sprintf("-%d", signal),
		"--",
		fmt.Sprintf("-%d", pid),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("signal process group with sudo (signal %d): %w: %s", signal, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (p *nodeAgentProcess) stop() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	p.once.Do(func() {
		select {
		case <-p.done:
			return
		default:
		}

		var signalErrors []string
		if err := p.signalGroup(syscall.SIGTERM); err != nil {
			signalErrors = append(signalErrors, err.Error())
		}

		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			if err := p.signalGroup(syscall.SIGKILL); err != nil {
				signalErrors = append(signalErrors, err.Error())
			}
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				details := "no signal errors"
				if len(signalErrors) > 0 {
					details = strings.Join(signalErrors, "; ")
				}
				p.stopErr = fmt.Errorf(
					"Node Agent process group %d survived SIGTERM and SIGKILL; %s; agent output:\n%s",
					p.cmd.Process.Pid,
					details,
					p.output.String(),
				)
				return
			}
		}

		if len(signalErrors) > 0 {
			p.stopErr = fmt.Errorf(
				"Node Agent process cleanup required signal errors: %s; agent output:\n%s",
				strings.Join(signalErrors, "; "),
				p.output.String(),
			)
		}
	})
	return p.stopErr
}

func waitForE2ECondition(
	t *testing.T,
	ctx context.Context,
	agent *nodeAgentProcess,
	description string,
	check func() (bool, error),
) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-agent.done:
			t.Fatalf("node-agent process stopped before %s: %v\nagent output:\n%s", description, agent.waitErr, agent.output.String())
		default:
		}
		ok, err := check()
		if err == nil && ok {
			return
		}
		if err != nil {
			t.Logf("waiting for %s; transient check error: %v", description, err)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s: %v\nagent output:\n%s", description, ctx.Err(), agent.output.String())
		case <-ticker.C:
		}
	}
}
