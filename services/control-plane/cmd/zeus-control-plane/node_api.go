package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"crypto/x509"
	"regexp"
	"strings"
	"time"
)

type NodeAPI struct {
	store  NodeAgentStore
	logger *slog.Logger
}

type nodeAckRequest struct {
	Status    string "json:\"status\""
	ErrorCode string "json:\"error_code,omitempty\""
}

var commandErrorCodePattern = regexp.MustCompile("^(validation_failed|config_rejected|wg_apply_failed|command_expired|internal_error|mutation_disabled)$")

func NewNodeAgentHandler(store NodeAgentStore, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	api := &NodeAPI{store: store, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /agent/v1/commands/next", api.handleNextCommand)
	mux.HandleFunc("POST /agent/v1/commands/{commandID}/ack", api.handleAcknowledge)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("node-agent request panic recovered", "request_id", requestID)
				writeError(w, http.StatusInternalServerError, "internal_error", "خطای داخلی سرور", requestID)
			}
		}()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *NodeAPI) authenticateNode(w http.ResponseWriter, r *http.Request) (NodeIdentity, bool) {
	if a.store == nil || r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 {
		writeError(w, http.StatusUnauthorized, "node_unauthorized", "گواهی معتبر نود ارائه نشده است", requestIDFrom(r))
		return NodeIdentity{}, false
	}
	leaf := r.TLS.PeerCertificates[0]
	if leaf.IsCA {
		writeError(w, http.StatusUnauthorized, "node_unauthorized", "گواهی معتبر نود ارائه نشده است", requestIDFrom(r))
		return NodeIdentity{}, false
	}
	hasClientAuthUsage := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuthUsage = true
			break
		}
	}
	if !hasClientAuthUsage {
		writeError(w, http.StatusUnauthorized, "node_unauthorized", "گواهی معتبر نود ارائه نشده است", requestIDFrom(r))
		return NodeIdentity{}, false
	}
	fingerprint := sha256.Sum256(leaf.Raw)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	node, err := a.store.FindNodeByCertFingerprint(ctx, fingerprint[:])
	if err != nil {
		if !errors.Is(err, ErrNodeNotFound) {
			a.logger.Error("node certificate lookup failed", "request_id", requestIDFrom(r))
		}
		writeError(w, http.StatusUnauthorized, "node_unauthorized", "گواهی معتبر نود ارائه نشده است", requestIDFrom(r))
		return NodeIdentity{}, false
	}
	return node, true
}

func (a *NodeAPI) handleNextCommand(w http.ResponseWriter, r *http.Request) {
	node, ok := a.authenticateNode(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	command, err := a.store.LeaseNodeCommand(ctx, node.ID, 45*time.Second)
	if errors.Is(err, ErrNoNodeCommand) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		a.logger.Error("node command lease failed", "request_id", requestIDFrom(r), "node_id", node.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "دریافت فرمان انجام نشد", requestIDFrom(r))
		return
	}
	writeJSON(w, http.StatusOK, command)
}

func (a *NodeAPI) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	node, ok := a.authenticateNode(w, r)
	if !ok {
		return
	}
	commandID := r.PathValue("commandID")
	if !uuidPattern.MatchString(commandID) {
		writeError(w, http.StatusBadRequest, "invalid_command_id", "شناسه فرمان معتبر نیست", requestIDFrom(r))
		return
	}
	var input nodeAckRequest
	if err := decodeJSON(w, r, &input, 4*1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_acknowledgement", "پاسخ نود معتبر نیست", requestIDFrom(r))
		return
	}
	input.Status = strings.TrimSpace(input.Status)
	input.ErrorCode = strings.TrimSpace(input.ErrorCode)
	if input.Status != "succeeded" && input.Status != "failed" {
		writeError(w, http.StatusBadRequest, "invalid_acknowledgement", "وضعیت پاسخ نود معتبر نیست", requestIDFrom(r))
		return
	}
	if (input.Status == "failed" && !commandErrorCodePattern.MatchString(input.ErrorCode)) ||
		(input.Status == "succeeded" && input.ErrorCode != "") {
		writeError(w, http.StatusBadRequest, "invalid_acknowledgement", "کد خطای پاسخ نود معتبر نیست", requestIDFrom(r))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err := a.store.AcknowledgeNodeCommand(ctx, node.ID, commandID, input.Status, input.ErrorCode)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
	case errors.Is(err, ErrNodeCommandNotFound):
		writeError(w, http.StatusNotFound, "command_not_found", "فرمان پیدا نشد", requestIDFrom(r))
	case errors.Is(err, ErrNodeCommandLeaseExpired):
		writeError(w, http.StatusConflict, "command_lease_expired", "مهلت اجرای فرمان به پایان رسیده است", requestIDFrom(r))
	case errors.Is(err, ErrNodeCommandConflict):
		writeError(w, http.StatusConflict, "command_state_conflict", "وضعیت فرمان اجازه ثبت این پاسخ را نمی‌دهد", requestIDFrom(r))
	default:
		a.logger.Error("node command acknowledgement failed", "request_id", requestIDFrom(r), "node_id", node.ID, "command_id", commandID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "ثبت پاسخ نود انجام نشد", requestIDFrom(r))
	}
}
