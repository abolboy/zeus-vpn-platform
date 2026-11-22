package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTorClientEnrollmentRejectsWireGuardAndCallerOwnedFields(t *testing.T) {
	handler := newTestAPI(t, newFakeControlStore(Operator{
		ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
		Login: "owner", Role: "owner", Status: "active",
	}))
	tests := []struct {
		name string
		field string
	}{
		{name: "WireGuard public key", field: `"public_key":"BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc="`},
		{name: "caller supplied user ID", field: `"user_id":"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47"`},
		{name: "caller supplied node ID", field: `"node_id":"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"device_ref":"android-device-ref-0001","device_name":"ZEUS Android","platform":"android","client_version":"1.0.0",` + test.field + `}`)
			request := httptest.NewRequest(http.MethodPost, "/v1/client/tor/devices", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer not-a-real-token")
			request.Header.Set("Idempotency-Key", "tor-enrollment-key-0001")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_request") {
				t.Fatalf("status=%d body=%s; expected strict schema rejection", response.Code, response.Body.String())
			}
		})
	}
}

func TestTorClientEnrollmentRejectsNonAndroidPlatform(t *testing.T) {
	handler := newTestAPI(t, newFakeControlStore(Operator{
		ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
		Login: "owner", Role: "owner", Status: "active",
	}))
	body := []byte(`{"device_ref":"android-device-ref-0001","device_name":"ZEUS Android","platform":"ios","client_version":"1.0.0"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/client/tor/devices", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer not-a-real-token")
	request.Header.Set("Idempotency-Key", "tor-enrollment-key-0001")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_client") {
		t.Fatalf("status=%d body=%s; expected Android-only platform validation", response.Code, response.Body.String())
	}
}

func TestValidTorClientVersion(t *testing.T) {
	for _, value := range []string{"1.0.0", "2026.11.22-beta.1", "0.1+build.4"} {
		if !validTorClientVersion(value) {
			t.Errorf("valid version %q rejected", value)
		}
	}
	for _, value := range []string{"", "   ", strings.Repeat("v", 65), "1.0.0\nmalicious"} {
		if validTorClientVersion(value) {
			t.Errorf("invalid version %q accepted", value)
		}
	}
}

func TestTorClientEnrollmentFailsClosedWhenCustomerOIDCIsNotConfigured(t *testing.T) {
	handler := newTestAPI(t, newFakeControlStore(Operator{
		ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
		Login: "owner", Role: "owner", Status: "active",
	}))
	body := []byte(`{"device_ref":"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47","device_name":"ZEUS Android","platform":"android","client_version":"1.0.0"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/client/tor/devices", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer not-a-real-token")
	request.Header.Set("Idempotency-Key", "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), "customer_identity_not_configured") {
		t.Fatalf("status=%d body=%s; expected fail-closed OIDC configuration error", response.Code, response.Body.String())
	}
}

func TestTorClientEnrollmentRequiresIdempotencyKey(t *testing.T) {
	handler := newTestAPI(t, newFakeControlStore(Operator{
		ID: "8e09c3da-fbac-4b34-9ad0-090e1f8d4c47",
		Login: "owner", Role: "owner", Status: "active",
	}))
	body := []byte(`{"device_ref":"8e09c3da-fbac-4b34-9ad0-090e1f8d4c47","device_name":"ZEUS Android","platform":"android","client_version":"1.0.0"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/client/tor/devices", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer not-a-real-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), "idempotency_key_required") {
		t.Fatalf("status=%d body=%s; expected idempotency key validation", response.Code, response.Body.String())
	}
}
