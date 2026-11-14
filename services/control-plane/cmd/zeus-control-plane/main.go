package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	version := envOrDefault("ZEUS_VERSION", "0.4.0-alpha")
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
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			hasOwner, ownerErr := postgres.HasActiveOwner(ctx)
			cancel()
			if ownerErr != nil || !hasOwner {
				logger.Error("an active owner is required; set bootstrap credentials for first production startup", "error", ownerErr)
				os.Exit(1)
			}
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
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			hasOwner, ownerErr := postgres.HasActiveOwner(ctx)
			cancel()
			if ownerErr != nil || !hasOwner {
				logger.Error("no active owner account is available after bootstrap", "error", ownerErr)
				os.Exit(1)
			}
			logger.Info("bootstrap admin checked; an existing account was not overwritten")
		}
	} else {
		logger.Warn("database URL not configured; only liveness and metadata endpoints are available")
	}

	allowedOrigins := strings.Split(os.Getenv("ZEUS_ALLOWED_ORIGINS"), ",")
	if production && strings.TrimSpace(os.Getenv("ZEUS_ALLOWED_ORIGINS")) == "" {
		logger.Error("ZEUS_ALLOWED_ORIGINS must contain one or more exact browser origins in production")
		os.Exit(1)
	}
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

	var nodeServer *http.Server
	var nodeListener net.Listener
	nodeCertFile := os.Getenv("ZEUS_NODE_TLS_CERT_FILE")
	nodeKeyFile := os.Getenv("ZEUS_NODE_TLS_KEY_FILE")
	nodeCAFile := os.Getenv("ZEUS_NODE_CA_FILE")
	nodeTLSConfigured := nodeCertFile != "" || nodeKeyFile != "" || nodeCAFile != ""
	if nodeTLSConfigured {
		if nodeCertFile == "" || nodeKeyFile == "" || nodeCAFile == "" {
			logger.Error("node mTLS requires ZEUS_NODE_TLS_CERT_FILE, ZEUS_NODE_TLS_KEY_FILE, and ZEUS_NODE_CA_FILE together")
			os.Exit(1)
		}
		nodeStore, ok := store.(NodeAgentStore)
		if !ok {
			logger.Error("node mTLS requires an initialized PostgreSQL node-agent store")
			os.Exit(1)
		}
		tlsConfig, err := loadNodeTLSConfig(nodeCertFile, nodeKeyFile, nodeCAFile)
		if err != nil {
			logger.Error("node mTLS configuration failed", "error", err)
			os.Exit(1)
		}
		api.SetNodeClientCAs(tlsConfig.ClientCAs)
		nodeAddress := envOrDefault("ZEUS_NODE_HTTP_ADDR", "127.0.0.1:8443")
		nodeListener, err = net.Listen("tcp", nodeAddress)
		if err != nil {
			logger.Error("node-agent listener could not bind", "error", err)
			os.Exit(1)
		}
		nodeServer = &http.Server{
			Handler: NewNodeAgentHandler(nodeStore, logger),
			TLSConfig: tlsConfig,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout: 30 * time.Second,
			MaxHeaderBytes: 64 * 1024,
		}
		logger.Info("node-agent mTLS listener configured", "address", nodeAddress)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	errorsCh := make(chan error, 2)
	go func() {
		logger.Info("ZEUS control plane starting", "address", address, "version", version)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsCh <- fmt.Errorf("admin listener: %w", err)
		}
	}()
	if nodeServer != nil {
		go func() {
			if err := nodeServer.Serve(tls.NewListener(nodeListener, nodeServer.TLSConfig)); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errorsCh <- fmt.Errorf("node mTLS listener: %w", err)
			}
		}()
	}
	select {
	case sig := <-stop:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-errorsCh:
		logger.Error("HTTP server stopped unexpectedly", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("admin listener shutdown failed", "error", err)
	}
	if nodeServer != nil {
		if err := nodeServer.Shutdown(ctx); err != nil {
			logger.Error("node listener shutdown failed", "error", err)
		}
	}
}

func loadNodeTLSConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	certPEM, err := readTLSFile(certFile, false)
	if err != nil {
		return nil, fmt.Errorf("read node listener certificate: %w", err)
	}
	keyPEM, err := readTLSFile(keyFile, true)
	if err != nil {
		return nil, fmt.Errorf("read node listener private key: %w", err)
	}
	serverCertificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse node listener certificate and key: %w", err)
	}
	caPEM, err := readTLSFile(caFile, false)
	if err != nil {
		return nil, fmt.Errorf("read node client CA bundle: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("node client CA bundle contains no valid certificates")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: clientCAs,
		Certificates: []tls.Certificate{serverCertificate},
	}, nil
}

func readTLSFile(path string, private bool) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return nil, errors.New("could not open TLS file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("TLS material must be a regular file")
	}
	if private && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("node TLS private key must be owner-only (0600 or stricter)")
	}
	return io.ReadAll(file)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
