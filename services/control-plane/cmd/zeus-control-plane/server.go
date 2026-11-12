package main

import (
    "encoding/json"
    "net/http"
)

func NewHandler(version string) http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
    })
    mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
        // Process-only readiness; database and node checks are not implemented yet.
        writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
    })
    mux.HandleFunc("GET /v1/meta", func(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, http.StatusOK, map[string]string{
            "product": "ZEUS VPN", "service": "control-plane",
            "apiVersion": "v1", "version": version, "stage": "foundation",
        })
    })
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Cache-Control", "no-store")
        w.Header().Set("Referrer-Policy", "no-referrer")
        w.Header().Set("X-Frame-Options", "DENY")
        mux.ServeHTTP(w, r)
    })
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}
