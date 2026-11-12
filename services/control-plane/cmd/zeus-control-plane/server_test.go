package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestHealthAndReadiness(t *testing.T) {
    handler := NewHandler("test")
    for _, test := range []struct{ path, want string }{
        {"/healthz", "ok"},
        {"/readyz", "ready"},
    } {
        t.Run(test.path, func(t *testing.T) {
            response := httptest.NewRecorder()
            handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
            if response.Code != http.StatusOK {
                t.Fatalf("status = %d", response.Code)
            }
            var body map[string]string
            if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
                t.Fatalf("decode response: %v", err)
            }
            if body["status"] != test.want {
                t.Fatalf("status field = %q, want %q", body["status"], test.want)
            }
            if response.Header().Get("Cache-Control") != "no-store" {
                t.Fatal("missing no-store header")
            }
            if response.Header().Get("X-Content-Type-Options") != "nosniff" {
                t.Fatal("missing nosniff header")
            }
        })
    }
}

func TestMeta(t *testing.T) {
    response := httptest.NewRecorder()
    NewHandler("0.1.0-test").ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/meta", nil))
    var body map[string]string
    if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
        t.Fatal(err)
    }
    if body["product"] != "ZEUS VPN" || body["version"] != "0.1.0-test" || body["apiVersion"] != "v1" {
        t.Fatalf("unexpected metadata: %+v", body)
    }
}

func TestUnsupportedMethod(t *testing.T) {
    response := httptest.NewRecorder()
    NewHandler("test").ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/healthz", nil))
    if response.Code != http.StatusMethodNotAllowed {
        t.Fatalf("status = %d", response.Code)
    }
}
