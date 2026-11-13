package controlplaneclient

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testCertificates struct {
	caFile      string
	serverCert  tls.Certificate
	clientCert  tls.Certificate
	clientCertFile string
	clientKeyFile string
}

func TestClientUsesMutualTLSAndPollsCommands(t *testing.T) {
	certs := makeCertificates(t)
	var mu sync.Mutex
	acknowledgements := 0
	polls := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
			http.Error(w, "missing node client certificate", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/agent/v1/commands/next":
			mu.Lock()
			polls++
			current := polls
			mu.Unlock()
			if current == 1 {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(Command{
					ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
					NodeID: "7e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
					Type: "peer.ensure",
					Payload: json.RawMessage([]byte("{\"public_key\":\"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\",\"ipv4\":\"10.77.77.3\",\"ipv6\":\"fd77:77:77::3\"}")),
					Attempt: 1,
					ExpiresAt: time.Now().Add(5*time.Minute),
				})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/agent/v1/commands/8e09c3da-fbac-4b34-9ad0-090e1f8d4c47/ack":
			var ack struct {
				Status string "json:\"status\""
				ErrorCode string "json:\"error_code\""
			}
			if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
				http.Error(w, "bad acknowledgement", http.StatusBadRequest)
				return
			}
			if ack.Status != "succeeded" || ack.ErrorCode != "" {
				http.Error(w, "unexpected acknowledgement", http.StatusBadRequest)
				return
			}
			mu.Lock()
			acknowledgements++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"status\":\"acknowledged\"}"))
		default:
			http.NotFound(w, r)
		}
	}))
	server.TLS = &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: certs.clientCAPool(t),
		Certificates: []tls.Certificate{certs.serverCert},
	}
	server.StartTLS()
	defer server.Close()

	client, err := New(Config{
		BaseURL: server.URL,
		ClientCertFile: certs.clientCertFile,
		ClientKeyFile: certs.clientKeyFile,
		ServerCAFile: certs.caFile,
		Timeout: 5*time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()

	command, err := client.Poll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if command == nil || command.Type != "peer.ensure" || command.Attempt != 1 {
		t.Fatalf("unexpected command: %+v", command)
	}
	if err := client.Acknowledge(t.Context(), command.ID, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	empty, err := client.Poll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if empty != nil {
		t.Fatalf("expected no command, got %+v", empty)
	}
	mu.Lock()
	defer mu.Unlock()
	if polls != 2 || acknowledgements != 1 {
		t.Fatalf("polls=%d acknowledgements=%d", polls, acknowledgements)
	}
}

func TestClientRejectsInsecureOrMalformedConfiguration(t *testing.T) {
	certs := makeCertificates(t)
	config := Config{
		BaseURL: "http://control.example",
		ClientCertFile: certs.clientCertFile,
		ClientKeyFile: certs.clientKeyFile,
		ServerCAFile: certs.caFile,
	}
	if _, err := New(config); err == nil {
		t.Fatal("HTTP control-plane URL was accepted")
	}
	config.BaseURL = "https://user:password@control.example"
	if _, err := New(config); err == nil {
		t.Fatal("URL credentials were accepted")
	}
	config.BaseURL = "https://control.example/path"
	if _, err := New(config); err == nil {
		t.Fatal("URL path component was accepted")
	}
	config.BaseURL = "https://control.example"
	config.ClientKeyFile = ""
	if _, err := New(config); err == nil {
		t.Fatal("missing client key file was accepted")
	}
}

func TestClientRejectsBroadPrivateKeyPermissions(t *testing.T) {
	certs := makeCertificates(t)
	if err := os.Chmod(certs.clientKeyFile, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(Config{
		BaseURL: "https://control.example",
		ClientCertFile: certs.clientCertFile,
		ClientKeyFile: certs.clientKeyFile,
		ServerCAFile: certs.caFile,
	})
	if err == nil {
		t.Fatal("group/world-readable client private key was accepted")
	}
}

func (c testCertificates) clientCAPool(t *testing.T) *x509.CertPool {
	t.Helper()
	caPEM, err := os.ReadFile(c.caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("could not load client CA")
	}
	return pool
}

func makeCertificates(t *testing.T) testCertificates {
	t.Helper()
	directory := t.TempDir()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{CommonName: "ZEUS test CA"},
		NotBefore: now.Add(-time.Hour),
		NotAfter: now.Add(24*time.Hour),
		IsCA: true,
		BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	caFile := writePEMFile(t, directory, "ca.pem", "CERTIFICATE", caDER)
	serverKey, serverCertFile, serverKeyFile := issueCertificate(t, directory, "server", ca, caKey, x509.ExtKeyUsageServerAuth, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	clientKey, clientCertFile, clientKeyFile := issueCertificate(t, directory, "client", ca, caKey, x509.ExtKeyUsageClientAuth, nil, nil)
	serverCert, err := tls.LoadX509KeyPair(serverCertFile, serverKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	_ = serverKey
	_ = clientKey
	return testCertificates{
		caFile: caFile,
		serverCert: serverCert,
		clientCert: clientCert,
		clientCertFile: clientCertFile,
		clientKeyFile: clientKeyFile,
	}
}

func issueCertificate(t *testing.T, directory, name string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, usage x509.ExtKeyUsage, dnsNames []string, ipAddresses []net.IP) (*ecdsa.PrivateKey, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(int64(len(name) + 2)),
		Subject: pkix.Name{CommonName: "ZEUS test " + name},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(12*time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{usage},
		DNSNames: dnsNames,
		IPAddresses: ipAddresses,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile := writePEMFile(t, directory, name+".pem", "CERTIFICATE", certificateDER)
	keyFile := writePEMFile(t, directory, name+"-key.pem", "PRIVATE KEY", privateDER)
	return key, certFile, keyFile
}

func writePEMFile(t *testing.T, directory, name, blockType string, data []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var _ = bytes.Compare
