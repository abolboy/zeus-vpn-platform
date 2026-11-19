package main

import (
	"log/slog"
	"net/http/httptest"
	"testing"
)

func TestClientIPUsesForwardedAddressOnlyFromTrustedProxy(t *testing.T) {
	api, err := NewAPI("test", nil, slog.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.SetTrustedProxyCIDRs("10.240.0.0/24, 10.240.1.0/24"); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("GET", "https://control.example/healthz", nil)
	request.RemoteAddr = "10.240.1.17:443"
	request.Header.Add("X-Forwarded-For", "198.51.100.10, 203.0.113.24")
	got := api.clientIP(request)
	if got == nil || *got != "203.0.113.24" {
		t.Fatalf("trusted ALB client IP = %v, want 203.0.113.24", got)
	}
}

func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	api, err := NewAPI("test", nil, slog.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.SetTrustedProxyCIDRs("10.240.1.0/24"); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("GET", "https://control.example/healthz", nil)
	request.RemoteAddr = "192.0.2.44:12345"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	got := api.clientIP(request)
	if got == nil || *got != "192.0.2.44" {
		t.Fatalf("untrusted peer address = %v, want 192.0.2.44", got)
	}
}

func TestClientIPDefaultsToDirectPeerAndRejectsInvalidProxyCIDRs(t *testing.T) {
	api, err := NewAPI("test", nil, slog.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "https://control.example/healthz", nil)
	request.RemoteAddr = "192.0.2.55:12345"
	request.Header.Set("X-Forwarded-For", "203.0.113.77")
	got := api.clientIP(request)
	if got == nil || *got != "192.0.2.55" {
		t.Fatalf("default client IP = %v, want direct peer 192.0.2.55", got)
	}

	if err := api.SetTrustedProxyCIDRs("not-a-cidr"); err == nil {
		t.Fatal("invalid trusted proxy CIDR was accepted")
	}
	if err := api.SetTrustedProxyCIDRs("0.0.0.0/0"); err == nil {
		t.Fatal("unrestricted trusted proxy CIDR was accepted")
	}
}

func TestClientIPFallsBackWhenTrustedProxyHeaderIsMalformed(t *testing.T) {
	api, err := NewAPI("test", nil, slog.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.SetTrustedProxyCIDRs("10.240.1.0/24"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "https://control.example/healthz", nil)
	request.RemoteAddr = "10.240.1.17:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.10, invalid")
	got := api.clientIP(request)
	if got == nil || *got != "10.240.1.17" {
		t.Fatalf("malformed forwarded header fallback = %v, want proxy peer 10.240.1.17", got)
	}
}
