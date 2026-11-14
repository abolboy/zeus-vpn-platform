package nodecert

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	MinNodeCertificateValidity = time.Hour
	MaxNodeCertificateValidity = 365 * 24 * time.Hour
	MinCAVerificationValidity  = 365 * 24 * time.Hour
	MaxCAValidity              = 20 * 365 * 24 * time.Hour
)

var nodeIDPattern = regexp.MustCompile("(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

type CertificateInfo struct {
	NodeID      string
	Fingerprint string
	NotBefore   time.Time
	NotAfter    time.Time
	Serial      string
}

func NewCA(validity time.Duration) ([]byte, []byte, error) {
	if validity < MinCAVerificationValidity || validity > MaxCAValidity {
		return nil, nil, fmt.Errorf("CA validity must be between %s and %s", MinCAVerificationValidity, MaxCAValidity)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA key: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "ZEUS VPN Node Agent Root CA", Organization: []string{"ZEUS VPN"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(validity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create CA certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encode CA private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func IssueNodeCertificate(caCertificatePEM, caPrivateKeyPEM []byte, nodeID string, validity time.Duration) ([]byte, []byte, CertificateInfo, error) {
	nodeID = strings.ToLower(strings.TrimSpace(nodeID))
	if !nodeIDPattern.MatchString(nodeID) {
		return nil, nil, CertificateInfo{}, errors.New("node ID must be a canonical UUID")
	}
	if validity < MinNodeCertificateValidity || validity > MaxNodeCertificateValidity {
		return nil, nil, CertificateInfo{}, fmt.Errorf("node certificate validity must be between %s and %s", MinNodeCertificateValidity, MaxNodeCertificateValidity)
	}

	ca, err := parseCertificate(caCertificatePEM)
	if err != nil {
		return nil, nil, CertificateInfo{}, fmt.Errorf("parse node CA certificate: %w", err)
	}
	if !ca.IsCA || !ca.BasicConstraintsValid || ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, nil, CertificateInfo{}, errors.New("configured node certificate is not a valid signing CA")
	}
	if err := ca.CheckSignatureFrom(ca); err != nil {
		return nil, nil, CertificateInfo{}, errors.New("node CA certificate must be self-signed")
	}
	now := time.Now().UTC().Truncate(time.Second)
	if now.Before(ca.NotBefore) || !now.Before(ca.NotAfter) {
		return nil, nil, CertificateInfo{}, errors.New("node CA certificate is not currently valid")
	}

	signer, err := parsePrivateKey(caPrivateKeyPEM)
	if err != nil {
		return nil, nil, CertificateInfo{}, fmt.Errorf("parse node CA private key: %w", err)
	}
	certPublicDER, err := x509.MarshalPKIXPublicKey(ca.PublicKey)
	if err != nil {
		return nil, nil, CertificateInfo{}, errors.New("encode CA public key failed")
	}
	keyPublicDER, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil || !bytes.Equal(certPublicDER, keyPublicDER) {
		return nil, nil, CertificateInfo{}, errors.New("node CA private key does not match its certificate")
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, CertificateInfo{}, fmt.Errorf("generate node client key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, CertificateInfo{}, err
	}
	notBefore := now.Add(-5 * time.Minute)
	if ca.NotBefore.After(notBefore) {
		notBefore = ca.NotBefore
	}
	notAfter := now.Add(validity)
	if ca.NotAfter.Before(notAfter) {
		notAfter = ca.NotAfter
	}
	if notAfter.Sub(notBefore) < MinNodeCertificateValidity {
		return nil, nil, CertificateInfo{}, errors.New("node CA expires too soon to issue a valid node certificate")
	}
	nodeURI, err := url.Parse("spiffe://zeus/node/" + nodeID)
	if err != nil {
		return nil, nil, CertificateInfo{}, errors.New("construct node identity URI failed")
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "zeus-node-" + nodeID, Organization: []string{"ZEUS VPN"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		URIs:                  []*url.URL{nodeURI},
		AuthorityKeyId:        ca.SubjectKeyId,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, ca, &clientKey.PublicKey, signer)
	if err != nil {
		return nil, nil, CertificateInfo{}, fmt.Errorf("issue node client certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(clientKey)
	if err != nil {
		return nil, nil, CertificateInfo{}, fmt.Errorf("encode node private key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	fingerprint := sha256.Sum256(certDER)
	return certPEM, keyPEM, CertificateInfo{
		NodeID:      nodeID,
		Fingerprint: hex.EncodeToString(fingerprint[:]),
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		Serial:      serial.Text(16),
	}, nil
}

func CertificateFingerprint(certificatePEM []byte) (string, error) {
	certificate, err := parseCertificate(certificatePEM)
	if err != nil {
		return "", err
	}
	fingerprint := sha256.Sum256(certificate.Raw)
	return hex.EncodeToString(fingerprint[:]), nil
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("expected exactly one PEM CERTIFICATE block")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return certificate, nil
}

func parsePrivateKey(data []byte) (crypto.Signer, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("expected exactly one unencrypted PKCS#8 PRIVATE KEY block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("private key does not implement crypto.Signer")
	}
	return signer, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		return nil, errors.New("generated an invalid zero certificate serial")
	}
	return serial, nil
}
