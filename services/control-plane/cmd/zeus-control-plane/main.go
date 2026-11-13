package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	address := envOrDefault("ZEUS_HTTP_ADDR", ":8080")
	version := envOrDefault("ZEUS_VERSION", "0.2.0-alpha")
	production := strings.EqualFold(os.Getenv("ZEUS_ENV"), "production")

	var store ControlStore
	var postgres *PostgresStore
	dsn := os.Getenv("ZEUS_DATABASE_URL")
	if production && dsn == "" {
		logger.Error("ZEUS_DATABASE_URL is required in production")
		os.Exit(1)
	}
	if dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		opened, err := OpenPostgres(ctx, dsn, production)
		cancel()
		if err != nil {
			logger.Error("database initialization failed", "error", err)
			os.Exit(1)
		}
		postgres = opened
		store = opened
		defer postgres.Close()

		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		err = store.Ping(ctx)
		cancel()
		if err != nil {
			logger.Error("database schema is not ready; run zeus-migrate first", "error", err)
			os.Exit(1)
		}

		bootstrapLogin := strings.TrimSpace(os.Getenv("ZEUS_BOOTSTRAP_ADMIN_LOGIN"))
		bootstrapPassword := os.Getenv("ZEUS_BOOTSTRAP_ADMIN_PASSWORD")
		if (bootstrapLogin == "") != (bootstrapPassword == "") {
			logger.Error("both bootstrap admin environment variables must be set together")
			os.Exit(1)
		}
		if production && bootstrapLogin == "" {
			logger.Error("bootstrap admin credentials are required for first production startup")
			os.Exit(1)
		}
		if bootstrapLogin != "" {
			if len(bootstrapLogin) > 128 || !validPasswordForBootstrap(bootstrapPassword) {
				logger.Error("bootstrap admin credentials do not meet the required policy")
				os.Exit(1)
			}
			ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
			err = postgres.EnsureBootstrapOperator(ctx, bootstrapLogin, bootstrapPassword)
			cancel()
			if err != nil {
				logger.Error("bootstrap admin initialization failed", "error", err)
				os.Exit(1)
			}
			logger.Info("bootstrap admin checked; an existing account was not overwritten")
		}
	} else {
		logger.Warn("database URL not configured; only liveness and metadata endpoints are available")
	}

	allowedOrigins := strings.Split(os.Getenv("ZEUS_ALLOWED_ORIGINS"), ",")
	api, err := NewAPI(version, store, logger, allowedOrigins)
	if err != nil {
		logger.Error("API initialization failed", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr: address, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	errorsCh := make(chan error, 1)
	go func() {
		logger.Info("ZEUS control plane starting", "address", address, "version", version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsCh <- err
		}
	}()
	select {
	case sig := <-stop:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errorsCh:
		logger.Error("HTTP server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
