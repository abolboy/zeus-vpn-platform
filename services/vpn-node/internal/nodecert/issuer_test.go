package nodecert

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"testing"
	"time"
)

func TestIssueNodeCertificateIsClientOnlyAndBoundToNodeID(t *testing.T) {
	caCertPEM, caKeyPEM, err := NewCA(10 * 365 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	nodeID := "7e09c3da-fbac-4b34-9ad0-090e1f8d4c47"
	certPEM, keyPEM, info, err := IssueNodeCertificate(caCertPEM, caKeyPEM, nodeID, 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if info.NodeID != nodeID || len(info.Fingerprint) != 64 || info.NotAfter.Sub(info.NotBefore) < 89*24*time.Hour {
		t.Fatalf("unexpected certificate info: %+v", info)
	}

	block, rest := pem.Decode(certPEM)
	if block == nil || len(rest) != 0 {
		t.Fatal("certificate PEM did not contain exactly one block")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.IsCA || leaf.KeyUsage != x509.KeyUsageDigitalSignature ||
		len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("certificate has unexpected usages: %+v", leaf)
	}
	expectedURI, _ := url.Parse("spiffe://zeus/node/" + nodeID)
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != expectedURI.String() {
		t.Fatalf("certificate URI SAN is not bound to node ID: %+v", leaf.URIs)
	}
	if leaf.Subject.CommonName != "zeus-node-"+nodeID {
		t.Fatalf("unexpected common name %q", leaf.Subject.CommonName)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("failed to add root CA")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("issued certificate did not verify for ClientAuth: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err == nil {
		t.Fatal("client certificate unexpectedly verified for ServerAuth")
	}

	keyBlock, rest := pem.Decode(keyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(rest) != 0 {
		t.Fatal("node private key is not a single PKCS#8 PEM block")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("unexpected private key type %T", parsedKey)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(leaf.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePublicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(publicDER) != string(privatePublicDER) {
		t.Fatal("issued certificate and private key do not match")
	}
	fingerprint, err := CertificateFingerprint(certPEM)
	if err != nil || fingerprint != info.Fingerprint {
		t.Fatalf("fingerprint mismatch: %q, %v", fingerprint, err)
	}
}

func TestIssueNodeCertificateRejectsInvalidInputs(t *testing.T) {
	caCertPEM, caKeyPEM, err := NewCA(10 * 365 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := IssueNodeCertificate(caCertPEM, caKeyPEM, "not-a-uuid", 24*time.Hour); err == nil {
		t.Fatal("invalid node ID was accepted")
	}
	if _, _, _, err := IssueNodeCertificate(caCertPEM, caKeyPEM, "7e09c3da-fbac-4b34-9ad0-090e1f8d4c47", 366*24*time.Hour); err == nil {
		t.Fatal("overlong node certificate validity was accepted")
	}
	_, wrongKeyPEM, err := NewCA(10 * 365 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := IssueNodeCertificate(caCertPEM, wrongKeyPEM, "7e09c3da-fbac-4b34-9ad0-090e1f8d4c47", 24*time.Hour); err == nil {
		t.Fatal("mismatched CA private key was accepted")
	}
}

func TestNewCARejectsUnsafeValidity(t *testing.T) {
	for _, validity := range []time.Duration{time.Hour, 20*365*24*time.Hour + time.Second} {
		if _, _, err := NewCA(validity); err == nil {
			t.Fatalf("unsafe CA validity %s was accepted", validity)
		}
	}
}
