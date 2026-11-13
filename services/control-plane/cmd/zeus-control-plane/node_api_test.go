package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeNodeAgentStore struct {
	fingerprint []byte
	command     NodeCommand
	ackStatus   string
	ackError    string
	ackCalls    int
}

func (s *fakeNodeAgentStore) FindNodeByCertFingerprint(_ context.Context, value []byte) (NodeIdentity, error) {
	if !bytes.Equal(value, s.fingerprint) {
		return NodeIdentity{}, ErrNodeNotFound
	}
	return NodeIdentity{ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47"}, nil
}

func (s *fakeNodeAgentStore) LeaseNodeCommand(_ context.Context, _ string, _ time.Duration) (NodeCommand, error) {
	return s.command, nil
}

func (s *fakeNodeAgentStore) AcknowledgeNodeCommand(_ context.Context, _, _, status, errorCode string) error {
	s.ackCalls++
	s.ackStatus = status
	s.ackError = errorCode
	return nil
}

func TestNodeAgentRoutesRequireVerifiedClientCertificate(t *testing.T) {
	certFiles := makeNodeTestCertificates(t)
	serverTLS, err := loadNodeTLSConfig(certFiles.serverCert, certFiles.serverKey, certFiles.caCert)
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, err := tls.LoadX509KeyPair(certFiles.clientCert, certFiles.clientKey)
	if err != nil {
		t.Fatal(err)
	}
	clientLeaf, err := x509.ParseCertificate(clientCertificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := sha256.Sum256(clientLeaf.Raw)
	store := &fakeNodeAgentStore{
		fingerprint: fingerprint[:],
		command: NodeCommand{
			ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
			NodeID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
			Type: "peer.ensure",
			Payload: []byte(\`{"public_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","ipv4":"10.77.77.3","ipv6":"fd77:77:77::3"}\`),
			Attempt: 1,
			ExpiresAt: time.Now().Add(5*time.Minute),
		},
	}
	server := httptest.NewUnstartedServer(NewNodeAgentHandler(store, nil))
	server.TLS = serverTLS
	server.StartTLS()
	defer server.Close()

	roots := x509.NewCertPool()
	caPEM, err := os.ReadFile(certFiles.caCert)
	if err != nil || !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("could not load test CA")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs: roots,
		ServerName: "localhost",
		Certificates: []tls.Certificate{clientCertificate},
	}}}
	response, err := client.Get(server.URL + "/agent/v1/commands/next")
	if err != nil {
		t.Fatalf("mTLS client request failed: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("poll status=%d, want 200", response.StatusCode)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("node command response must not be cacheable")
	}

	ackBody := bytes.NewBufferString(\`{"status":"succeeded"}\`)
	ackRequest, err := http.NewRequest(http.MethodPost, server.URL+"/agent/v1/commands/8e09c3da-fbac-4b34-9ad0-090e1f8d4c47/ack", ackBody)
	if err != nil {
		t.Fatal(err)
	}
	ackRequest.Header.Set("Content-Type", "application/json")
	ackResponse, err := client.Do(ackRequest)
	if err != nil {
		t.Fatalf("mTLS acknowledgement failed: %v", err)
	}
	ackResponse.Body.Close()
	if ackResponse.StatusCode != http.StatusOK || store.ackCalls != 1 || store.ackStatus != "succeeded" {
		t.Fatalf("ack response status=%d calls=%d status=%s", ackResponse.StatusCode, store.ackCalls, store.ackStatus)
	}

	invalidRequest, err := http.NewRequest(http.MethodPost, server.URL+"/agent/v1/commands/8e09c3da-fbac-4b34-9ad0-090e1f8d4c47/ack", bytes.NewBufferString(\`{"status":"failed","error_code":"run arbitrary shell"}\`))
	if err != nil {
		t.Fatal(err)
	}
	invalidResponse, err := client.Do(invalidRequest)
	if err != nil {
		t.Fatal(err)
	}
	invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusBadRequest || store.ackCalls != 1 {
		t.Fatalf("invalid error code response=%d store calls=%d", invalidResponse.StatusCode, store.ackCalls)
	}

	noCertificateClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs: roots,
		ServerName: "localhost",
	}}}
	if _, err := noCertificateClient.Get(server.URL + "/agent/v1/commands/next"); err == nil {
		t.Fatal("server accepted a connection without a client certificate")
	}
}

func TestNodeAgentRejectsCertificateNotRegisteredToNode(t *testing.T) {
	certFiles := makeNodeTestCertificates(t)
	serverTLS, err := loadNodeTLSConfig(certFiles.serverCert, certFiles.serverKey, certFiles.caCert)
	if err != nil {
		t.Fatal(err)
	}
	clientCertificate, err := tls.LoadX509KeyPair(certFiles.clientCert, certFiles.clientKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(NewNodeAgentHandler(&fakeNodeAgentStore{fingerprint: []byte("wrong")}, nil))
	server.TLS = serverTLS
	server.StartTLS()
	defer server.Close()

	roots := x509.NewCertPool()
	caPEM, _ := os.ReadFile(certFiles.caCert)
	roots.AppendCertsFromPEM(caPEM)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, ServerName: "localhost", Certificates: []tls.Certificate{clientCertificate},
	}}}
	response, err := client.Get(server.URL + "/agent/v1/commands/next")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unregistered certificate status=%d, want 401", response.StatusCode)
	}
}

type nodeTestCertFiles struct {
	caCert     string
	serverCert string
	serverKey  string
	clientCert string
	clientKey  string
}

func makeNodeTestCertificates(t *testing.T) nodeTestCertFiles {
	t.Helper()
	directory := t.TempDir()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{CommonName: "ZEUS Test Node CA"},
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
	caPath := writePEM(t, directory, "ca.pem", "CERTIFICATE", caDER)

	_, serverCertPath, serverKeyPath := issueNodeTestCertificate(t, directory, "server", ca, caKey, x509.ExtKeyUsageServerAuth, []string{"localhost"})
	_, clientCertPath, clientKeyPath := issueNodeTestCertificate(t, directory, "client", ca, caKey, x509.ExtKeyUsageClientAuth, nil)
	return nodeTestCertFiles{caCert: caPath, serverCert: serverCertPath, serverKey: serverKeyPath, clientCert: clientCertPath, clientKey: clientKeyPath}
}

func issueNodeTestCertificate(t *testing.T, directory, name string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, usage x509.ExtKeyUsage, dnsNames []string) (*ecdsa.PrivateKey, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(int64(len(name) + 10)),
		Subject: pkix.Name{CommonName: "ZEUS Test " + name},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(12*time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{usage},
		DNSNames: dnsNames,
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := writePEM(t, directory, name+".pem", "CERTIFICATE", certificateDER)
	keyPath := writePEM(t, directory, name+"-key.pem", "PRIVATE KEY", privateDER)
	return key, certPath, keyPath
}

func writePEM(t *testing.T, directory, name, blockType string, data []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	content := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data})
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
