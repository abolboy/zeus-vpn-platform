package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RegisterNodeCertificateInput struct {
	CertificatePEM string `json:"certificate_pem"`
}

func (a *API) SetNodeClientCAs(roots *x509.CertPool) {
	if roots == nil {
		a.nodeClientCAs = nil
		return
	}
	a.nodeClientCAs = roots.Clone()
}

func (a *API) handleListNodeCertificates(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	switch operator.Role {
	case "owner", "admin", "support", "read_only":
	default:
		writeError(w, http.StatusForbidden, "forbidden", "دسترسی کافی نیست", requestIDFrom(r))
		return
	}
	nodeID := strings.ToLower(r.PathValue("nodeID"))
	if !uuidPattern.MatchString(nodeID) {
		writeError(w, http.StatusBadRequest, "invalid_node_id", "شناسه سرور معتبر نیست", requestIDFrom(r))
		return
	}
	store, ok := a.store.(NodeCertificateStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "مدیریت گواهی سرور آماده نیست", requestIDFrom(r))
		return
	}
	certificates, err := store.ListNodeAgentCertificates(r.Context(), nodeID)
	if errors.Is(err, ErrNodeNotFound) {
		writeError(w, http.StatusNotFound, "node_not_found", "سرور پیدا نشد", requestIDFrom(r))
		return
	}
	if err != nil {
		a.logger.Error("node certificate list failed", "request_id", requestIDFrom(r), "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "فهرست گواهی‌ها دریافت نشد", requestIDFrom(r))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": certificates})
}

func (a *API) handleRegisterNodeCertificate(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	if !canManageUsers(operator.Role) {
		a.recordAudit(r, &operator.ID, "node.certificate.register", "denied")
		writeError(w, http.StatusForbidden, "forbidden", "فقط مدیران مجاز به ثبت گواهی هستند", requestIDFrom(r))
		return
	}
	nodeID := strings.ToLower(r.PathValue("nodeID"))
	if !uuidPattern.MatchString(nodeID) {
		writeError(w, http.StatusBadRequest, "invalid_node_id", "شناسه سرور معتبر نیست", requestIDFrom(r))
		return
	}
	var input RegisterNodeCertificateInput
	if err := decodeJSON(w, r, &input, 32*1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "گواهی ارسالی معتبر نیست", requestIDFrom(r))
		return
	}
	fingerprint, notBefore, notAfter, err := a.validateNodeCertificate(nodeID, input.CertificatePEM)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_node_certificate", "گواهی باید معتبر، صادرشده از CA مورداعتماد و مخصوص همین سرور باشد", requestIDFrom(r))
		return
	}
	store, ok := a.store.(NodeCertificateStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ذخیره‌ساز گواهی سرور آماده نیست", requestIDFrom(r))
		return
	}
	certificate, replayed, err := store.RegisterNodeAgentCertificate(
		r.Context(), operator.ID, nodeID, fingerprint, notBefore, notAfter,
		requestIDFrom(r), remoteIP(r.RemoteAddr),
	)
	switch {
	case errors.Is(err, ErrNodeNotFound):
		writeError(w, http.StatusNotFound, "node_not_found", "سرور پیدا نشد", requestIDFrom(r))
		return
	case errors.Is(err, ErrNodeCertificateConflict):
		writeError(w, http.StatusConflict, "certificate_conflict", "این گواهی قبلاً برای سرور دیگری ثبت شده یا اطلاعات آن متفاوت است", requestIDFrom(r))
		return
	case err != nil:
		a.logger.Error("node certificate registration failed", "request_id", requestIDFrom(r), "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "ثبت گواهی انجام نشد", requestIDFrom(r))
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
		writeJSON(w, http.StatusOK, certificate)
		return
	}
	writeJSON(w, http.StatusCreated, certificate)
}

func (a *API) handleRevokeNodeCertificate(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	if !canManageUsers(operator.Role) {
		a.recordAudit(r, &operator.ID, "node.certificate.revoke", "denied")
		writeError(w, http.StatusForbidden, "forbidden", "فقط مدیران مجاز به ابطال گواهی هستند", requestIDFrom(r))
		return
	}
	nodeID := strings.ToLower(r.PathValue("nodeID"))
	certificateID := strings.ToLower(r.PathValue("certificateID"))
	if !uuidPattern.MatchString(nodeID) || !uuidPattern.MatchString(certificateID) {
		writeError(w, http.StatusBadRequest, "invalid_certificate_id", "شناسه سرور یا گواهی معتبر نیست", requestIDFrom(r))
		return
	}
	store, ok := a.store.(NodeCertificateStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ذخیره‌ساز گواهی سرور آماده نیست", requestIDFrom(r))
		return
	}
	certificate, replayed, err := store.RevokeNodeAgentCertificate(
		r.Context(), operator.ID, nodeID, certificateID,
		requestIDFrom(r), remoteIP(r.RemoteAddr),
	)
	switch {
	case errors.Is(err, ErrNodeNotFound):
		writeError(w, http.StatusNotFound, "node_not_found", "سرور پیدا نشد", requestIDFrom(r))
		return
	case errors.Is(err, ErrNodeCertificateNotFound):
		writeError(w, http.StatusNotFound, "certificate_not_found", "گواهی پیدا نشد", requestIDFrom(r))
		return
	case err != nil:
		a.logger.Error("node certificate revocation failed", "request_id", requestIDFrom(r), "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "ابطال گواهی انجام نشد", requestIDFrom(r))
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusOK, certificate)
}

func (a *API) validateNodeCertificate(nodeID, certificatePEM string) ([]byte, time.Time, time.Time, error) {
	if a.nodeClientCAs == nil {
		return nil, time.Time{}, time.Time{}, errors.New("node client CA trust is not configured")
	}
	if len(certificatePEM) == 0 || len(certificatePEM) > 24*1024 {
		return nil, time.Time{}, time.Time{}, errors.New("certificate PEM size is invalid")
	}
	block, rest := pem.Decode([]byte(certificatePEM))
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, time.Time{}, time.Time{}, errors.New("exactly one PEM certificate is required")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, time.Time{}, time.Time{}, err
	}
	if leaf.IsCA || leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 ||
		len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		return nil, time.Time{}, time.Time{}, errors.New("certificate must be a non-CA ClientAuth-only leaf")
	}
	expectedURI, err := url.Parse("spiffe://zeus/node/" + nodeID)
	if err != nil || len(leaf.URIs) != 1 || leaf.URIs[0].String() != expectedURI.String() {
		return nil, time.Time{}, time.Time{}, errors.New("certificate URI SAN does not match the requested node")
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return nil, time.Time{}, time.Time{}, errors.New("certificate is not currently valid")
	}
	if leaf.NotAfter.Sub(leaf.NotBefore) > 365*24*time.Hour+10*time.Minute {
		return nil, time.Time{}, time.Time{}, errors.New("certificate validity exceeds the maximum")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: a.nodeClientCAs,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		CurrentTime: now,
	}); err != nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("verify node client certificate: %w", err)
	}
	sum := sha256.Sum256(leaf.Raw)
	fingerprint := make([]byte, len(sum))
	copy(fingerprint, sum[:])
	return fingerprint, leaf.NotBefore.UTC().Truncate(time.Microsecond), leaf.NotAfter.UTC().Truncate(time.Microsecond), nil
}
