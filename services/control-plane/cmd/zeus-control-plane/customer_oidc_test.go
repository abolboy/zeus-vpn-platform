package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const customerOIDCTestClientID = "zeus-android-public-test"

type customerOIDCTestProvider struct {
	server    *httptest.Server
	privateKey *rsa.PrivateKey
	issuer    string
	api       *API
	ctx       context.Context
}

func newCustomerOIDCTestProvider(t *testing.T) *customerOIDCTestProvider {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate OIDC test signing key: %v", err)
	}

	provider := &customerOIDCTestProvider{privateKey: privateKey}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                provider.issuer,
			"authorization_endpoint":               provider.issuer + "/authorize",
			"token_endpoint":                       provider.issuer + "/token",
			"userinfo_endpoint":                    provider.issuer + "/userinfo",
			"jwks_uri":                             provider.issuer + "/keys",
			"response_types_supported":             []string{"code", "id_token", "code id_token"},
			"subject_types_supported":              []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		exponent := big.NewInt(int64(privateKey.PublicKey.E)).Bytes()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kty": "RSA",
				"use": "sig",
				"kid": "zeus-oidc-test-key",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(exponent),
			}},
		})
	})
	provider.server = httptest.NewTLSServer(mux)
	t.Cleanup(provider.server.Close)
	provider.issuer = provider.server.URL
	provider.ctx = oidc.ClientContext(context.Background(), provider.server.Client())
	provider.api = &API{}
	if err := provider.api.ConfigureCustomerOIDC(provider.ctx, provider.issuer, customerOIDCTestClientID); err != nil {
		t.Fatalf("configure local OIDC test provider: %v", err)
	}
	return provider
}

func (p *customerOIDCTestProvider) signIDToken(t *testing.T, overrides map[string]any) string {
	t.Helper()
	now := time.Now().UTC()
	claims := map[string]any{
		"iss":            p.issuer,
		"sub":            "customer-subject-123",
		"aud":            customerOIDCTestClientID,
		"iat":            now.Add(-30 * time.Second).Unix(),
		"exp":            now.Add(5 * time.Minute).Unix(),
		"email":          "Customer@Example.com",
		"email_verified": true,
		"name":           "ZEUS Customer",
	}
	for key, value := range overrides {
		claims[key] = value
	}
	header, err := json.Marshal(map[string]string{
		"alg": "RS256",
		"kid": "zeus-oidc-test-key",
		"typ": "JWT",
	})
	if err != nil {
		t.Fatalf("encode OIDC test JWT header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("encode OIDC test JWT claims: %v", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, p.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign OIDC test JWT: %v", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestVerifyCustomerIdentityAcceptsVerifiedSignedIDToken(t *testing.T) {
	provider := newCustomerOIDCTestProvider(t)
	token := provider.signIDToken(t, nil)

	claims, externalRef, err := provider.api.verifyCustomerIdentity(provider.ctx, "Bearer "+token)
	if err != nil {
		t.Fatalf("verify valid customer ID token: %v", err)
	}
	if claims.Subject != "customer-subject-123" {
		t.Fatalf("subject = %q, want customer-subject-123", claims.Subject)
	}
	if claims.Email != "customer@example.com" || !claims.EmailVerified {
		t.Fatalf("verified email claims were not normalized: %+v", claims)
	}
	if externalRef != provider.issuer+"|customer-subject-123" {
		t.Fatalf("external identity reference = %q", externalRef)
	}
}

func TestVerifyCustomerIdentityRejectsInvalidIDTokens(t *testing.T) {
	provider := newCustomerOIDCTestProvider(t)
	now := time.Now().UTC()

	tests := []struct {
		name      string
		overrides map[string]any
	}{
		{
			name:      "unverified email",
			overrides: map[string]any{"email_verified": false},
		},
		{
			name:      "wrong audience",
			overrides: map[string]any{"aud": "another-client"},
		},
		{
			name:      "expired token",
			overrides: map[string]any{"iat": now.Add(-10 * time.Minute).Unix(), "exp": now.Add(-time.Minute).Unix()},
		},
		{
			name:      "wrong issuer",
			overrides: map[string]any{"iss": provider.issuer + "/other"},
		},
		{
			name:      "missing subject",
			overrides: map[string]any{"sub": ""},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := provider.signIDToken(t, test.overrides)
			if _, _, err := provider.api.verifyCustomerIdentity(provider.ctx, "Bearer "+token); err == nil {
				t.Fatal("expected invalid customer ID token to be rejected")
			}
		})
	}
}

func TestConfigureCustomerOIDCRequiresHTTPS(t *testing.T) {
	api := &API{}
	if err := api.ConfigureCustomerOIDC(context.Background(), "http://identity.example.test", customerOIDCTestClientID); err == nil {
		t.Fatal("expected a non-HTTPS OIDC issuer to be rejected")
	}
}

func TestConfigureCustomerOIDCPreservesExactIssuer(t *testing.T) {
	provider := newCustomerOIDCTestProvider(t)
	if provider.api.customerIssuer != provider.issuer {
		t.Fatalf("configured issuer = %q, want exact issuer %q", provider.api.customerIssuer, provider.issuer)
	}

}
