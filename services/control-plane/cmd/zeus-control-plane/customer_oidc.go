package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type customerIdentityClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

func (a *API) ConfigureCustomerOIDC(ctx context.Context, issuer, clientID string) error {
	issuer = strings.TrimSpace(issuer)
	clientID = strings.TrimSpace(clientID)
	if issuer == "" || clientID == "" || !strings.HasPrefix(issuer, "https://") {
		return errors.New("customer OIDC requires an HTTPS issuer and a client ID")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return fmt.Errorf("discover customer identity provider: %w", err)
	}
	a.customerVerifier = provider.Verifier(&oidc.Config{ClientID: clientID})
	a.customerIssuer = strings.TrimRight(issuer, "/")
	return nil
}

func (a *API) verifyCustomerIdentity(ctx context.Context, authorizationHeader string) (customerIdentityClaims, string, error) {
	scheme, token, ok := strings.Cut(authorizationHeader, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return customerIdentityClaims{}, "", errors.New("customer identity token is missing")
	}
	if a.customerVerifier == nil {
		return customerIdentityClaims{}, "", errors.New("customer identity provider is not configured")
	}
	idToken, err := a.customerVerifier.Verify(ctx, token)
	if err != nil {
		return customerIdentityClaims{}, "", fmt.Errorf("verify customer ID token: %w", err)
	}
	if idToken.Issuer != a.customerIssuer || idToken.Subject == "" || len(idToken.Subject) > 255 {
		return customerIdentityClaims{}, "", errors.New("customer identity issuer or subject is invalid")
	}
	var claims customerIdentityClaims
	if err := idToken.Claims(&claims); err != nil {
		return customerIdentityClaims{}, "", fmt.Errorf("read customer identity claims: %w", err)
	}
	claims.Subject = idToken.Subject
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if !claims.EmailVerified || claims.Email == "" || len(claims.Email) > 254 {
		return customerIdentityClaims{}, "", errors.New("verified customer email is required")
	}
	if claims.Name = strings.TrimSpace(claims.Name); claims.Name == "" {
		claims.Name = claims.Email
	}
	if len(claims.Name) > 120 {
		claims.Name = claims.Name[:120]
	}
	return claims, idToken.Issuer + "|" + idToken.Subject, nil
}

func (a *API) handleCustomerClientEnrollment(w http.ResponseWriter, r *http.Request) {
	requestID := requestIDFrom(r)
	ip := remoteIP(r.RemoteAddr)
	limiterKey := "unknown"
	if ip != nil {
		limiterKey = *ip
	}
	if a.enrollmentLimiter == nil || !a.enrollmentLimiter.allow(limiterKey, time.Now()) {
		w.Header().Set("Retry-After", "300")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "تعداد تلاش‌های ثبت دستگاه بیش از حد مجاز است", requestID)
		return
	}
	if a.customerVerifier == nil {
		writeError(w, http.StatusServiceUnavailable, "customer_identity_not_configured", "ورود و ثبت‌نام مشتری هنوز پیکربندی نشده است", requestID)
		return
	}

	claims, externalRef, err := a.verifyCustomerIdentity(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "customer_identity_invalid", "نشست مشتری معتبر یا تأییدشده نیست", requestID)
		return
	}
	var input ClientEnrollmentInput
	if err := decodeJSON(w, r, &input, 16*1024); err != nil || strings.TrimSpace(input.ActivationCode) != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات ثبت دستگاه معتبر نیست", requestID)
		return
	}
	deviceInput := CreateDeviceInput{
		DeviceRef: strings.TrimSpace(input.DeviceRef),
		DeviceName: strings.TrimSpace(input.DeviceName),
		Platform: strings.ToLower(strings.TrimSpace(input.Platform)),
		PublicKey: strings.TrimSpace(input.PublicKey),
	}
	if !deviceRefPattern.MatchString(deviceInput.DeviceRef) {
		writeError(w, http.StatusBadRequest, "invalid_device_ref", "شناسه دستگاه معتبر نیست", requestID)
		return
	}
	if !validDisplayName(deviceInput.DeviceName) {
		writeError(w, http.StatusBadRequest, "invalid_device_name", "نام دستگاه باید بین ۱ تا ۸۰ نویسه باشد", requestID)
		return
	}
	switch deviceInput.Platform {
	case "android", "ios", "windows", "macos", "linux", "other":
	default:
		writeError(w, http.StatusBadRequest, "invalid_platform", "سیستم‌عامل دستگاه معتبر نیست", requestID)
		return
	}
	if !validWireGuardPublicKey(deviceInput.PublicKey) {
		writeError(w, http.StatusBadRequest, "invalid_public_key", "کلید عمومی WireGuard معتبر نیست", requestID)
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "کلید یکتای درخواست معتبر نیست", requestID)
		return
	}
	store, ok := a.store.(CustomerSelfServiceStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ثبت‌نام مشتری آماده نیست", requestID)
		return
	}
	user, err := store.EnsureExternalVPNUser(r.Context(), externalRef, claims.Name)
	if err != nil {
		a.logger.Error("customer account provisioning failed", "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "customer_account_unavailable", "ایجاد حساب مشتری انجام نشد", requestID)
		return
	}
	if user.Status != "active" {
		writeError(w, http.StatusForbidden, "customer_account_inactive", "حساب مشتری فعال نیست", requestID)
		return
	}
	requestBody, err := json.Marshal(deviceInput)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات ثبت دستگاه معتبر نیست", requestID)
		return
	}
	requestHash := sha256.Sum256(requestBody)
	device, replayed, err := store.EnrollCustomerVPNDevice(
		r.Context(), user.ID, idempotencyKey, requestHash[:], deviceInput, requestID, ip,
	)
	switch {
	case errors.Is(err, ErrDeviceLimitReached):
		writeError(w, http.StatusConflict, "device_limit_reached", "سقف دستگاه‌های این حساب تکمیل شده است", requestID)
	case errors.Is(err, ErrUserNotActive):
		writeError(w, http.StatusForbidden, "customer_account_inactive", "حساب مشتری فعال نیست", requestID)
	case errors.Is(err, ErrNoProvisioningNode):
		writeError(w, http.StatusServiceUnavailable, "node_unavailable", "هیچ سرور آماده‌ای برای تخصیص اتصال وجود ندارد", requestID)
	case errors.Is(err, ErrAddressPoolExhausted):
		writeError(w, http.StatusServiceUnavailable, "address_pool_exhausted", "ظرفیت آدرس سرور تکمیل شده است", requestID)
	case errors.Is(err, ErrDeviceConflict):
		writeError(w, http.StatusConflict, "device_conflict", "این دستگاه قبلاً ثبت شده یا در انتظار تکمیل است", requestID)
	case errors.Is(err, ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "شناسه درخواست قبلاً با اطلاعات دیگری استفاده شده است", requestID)
	case err != nil:
		a.logger.Error("customer device enrollment failed", "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "enrollment_failed", "ثبت دستگاه انجام نشد", requestID)
	default:
		if replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusAccepted, device)
	}
}
