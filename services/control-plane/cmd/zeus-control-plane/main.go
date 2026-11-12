package main

import (
    "context"
    "errors"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func main() {
    address := envOrDefault("ZEUS_HTTP_ADDR", ":8080")
    version := envOrDefault("ZEUS_VERSION", "0.1.0-dev")
    server := &http.Server{
        Addr: address, Handler: NewHandler(version),
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout: 15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout: 60 * time.Second,
    }
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
    errorsCh := make(chan error, 1)
    go func() {
        slog.Info("ZEUS control plane starting", "address", address, "version", version)
        if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            errorsCh <- err
        }
    }()
    select {
    case sig := <-stop:
        slog.Info("shutdown signal received", "signal", sig.String())
    case err := <-errorsCh:
        slog.Error("HTTP server stopped unexpectedly", "error", err)
        os.Exit(1)
    }
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := server.Shutdown(ctx); err != nil {
        slog.Error("graceful shutdown failed", "error", err)
        os.Exit(1)
    }
}

func envOrDefault(key, fallback string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return fallback
}
