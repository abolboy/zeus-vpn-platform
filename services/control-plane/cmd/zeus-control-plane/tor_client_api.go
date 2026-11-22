package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type torClientEnrollmentRequest struct {
	DeviceRef    string `json:"device_ref"`
	DeviceName   string `json:"device_name"`
	Platform     string `json:"platform"`
	ClientVersion string `json:"client_version"`
}

func validTorClientVersion(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (a *API) handleCreateTorEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	if !canManageUsers(operator.Role) {
		a.recordAudit(r, &operator.ID, "tor.device.enrollment_code.create", "denied")
		writeError(w, http.StatusForbidden, "forbidden", "فقط مدیران مجاز به صدور دعوت‌نامه Tor هستند", requestIDFrom(r))
		return
	}
	userID := r.PathValue("userID")
	if !uuidPattern.MatchString(userID) {
		writeError(w, http.StatusBadRequest, "invalid_user_id", "شناسه کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	store, ok := a.store.(TorClientStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ثبت‌نام دستگاه Tor آماده نیست", requestIDFrom(r))
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		a.logger.Error("Tor invitation generation failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "internal_error", "صدور دعوت‌نامه انجام نشد", requestIDFrom(r))
		return
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	for i := range raw {
		raw[i] = 0
	}
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	err := store.CreateTorEnrollmentCode(
		r.Context(), operator.ID, userID, hash[:], expiresAt,
		requestIDFrom(r), remoteIP(r.RemoteAddr),
	)
	switch {
	case errors.Is(err, ErrVPNUserNotFound):
		writeError(w, http.StatusNotFound, "user_not_found", "کاربر پیدا نشد", requestIDFrom(r))
	case errors.Is(err, ErrUserNotActive):
		writeError(w, http.StatusConflict, "user_not_active", "برای حساب غیرفعال نمی‌توان دعوت‌نامه صادر کرد", requestIDFrom(r))
	case err != nil:
		a.logger.Error("Tor invitation persistence failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "internal_error", "صدور دعوت‌نامه انجام نشد", requestIDFrom(r))
	default:
		writeJSON(w, http.StatusCreated, EnrollmentCodeResponse{Code: code, ExpiresAt: expiresAt})
	}
}

func (a *API) handleTorClientEnrollment(w http.ResponseWriter, r *http.Request) {
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

	var input torClientEnrollmentRequest
	if err := decodeJSON(w, r, &input, 8*1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات ثبت دستگاه Tor معتبر نیست", requestID)
		return
	}
	input.DeviceRef = strings.TrimSpace(input.DeviceRef)
	input.DeviceName = strings.TrimSpace(input.DeviceName)
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.ClientVersion = strings.TrimSpace(input.ClientVersion)
	if !deviceRefPattern.MatchString(input.DeviceRef) {
		writeError(w, http.StatusBadRequest, "invalid_device_ref", "شناسه دستگاه معتبر نیست", requestID)
		return
	}
	if !validDisplayName(input.DeviceName) {
		writeError(w, http.StatusBadRequest, "invalid_device_name", "نام دستگاه باید بین ۱ تا ۸۰ نویسه باشد", requestID)
		return
	}
	if input.Platform != "android" || !validTorClientVersion(input.ClientVersion) {
		writeError(w, http.StatusBadRequest, "invalid_client", "سکوی دستگاه یا نسخه برنامه معتبر نیست", requestID)
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "کلید یکتای درخواست معتبر نیست", requestID)
		return
	}
	store, ok := a.store.(TorClientStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ثبت‌نام دستگاه Tor آماده نیست", requestID)
		return
	}

	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, credential, hasCredential := strings.Cut(authorization, " ")
	if !hasCredential || credential == "" || strings.ContainsAny(credential, " \t\r\n") {
		writeError(w, http.StatusUnauthorized, "customer_identity_invalid", "نشست یا دعوت‌نامه معتبر نیست", requestID)
		return
	}

	var userID string
	var invitationHash []byte
	switch {
	case strings.EqualFold(scheme, "Bearer"):
		claims, externalRef, err := a.verifyCustomerIdentity(r.Context(), authorization)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "customer_identity_invalid", "نشست مشتری معتبر یا تأییدشده نیست", requestID)
			return
		}
		user, err := store.EnsureExternalVPNUser(r.Context(), externalRef, claims.Name)
		if err != nil {
			a.logger.Error("Tor customer account lookup failed", "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "customer_account_unavailable", "دریافت حساب مشتری انجام نشد", requestID)
			return
		}
		if user.Status != "active" {
			writeError(w, http.StatusForbidden, "customer_account_inactive", "حساب مشتری فعال نیست", requestID)
			return
		}
		userID = user.ID
	case strings.EqualFold(scheme, "EnrollmentCode"):
		hash, valid := activationCodeTokenHash(credential)
		if !valid {
			writeError(w, http.StatusUnauthorized, "enrollment_code_invalid", "دعوت‌نامه Tor معتبر نیست", requestID)
			return
		}
		invitationHash = hash[:]
		userID, err = store.FindTorEnrollmentCodeUser(r.Context(), invitationHash)
		switch {
		case errors.Is(err, ErrEnrollmentCodeExpired):
			writeError(w, http.StatusGone, "enrollment_code_expired", "مهلت دعوت‌نامه پایان یافته است", requestID)
			return
		case errors.Is(err, ErrEnrollmentCodeUsed):
			writeError(w, http.StatusConflict, "enrollment_code_used", "این دعوت‌نامه قبلاً مصرف شده است", requestID)
			return
		case errors.Is(err, ErrEnrollmentCodeInvalid):
			writeError(w, http.StatusUnauthorized, "enrollment_code_invalid", "دعوت‌نامه Tor معتبر نیست", requestID)
			return
		case err != nil:
			a.logger.Error("Tor invitation lookup failed", "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "enrollment_unavailable", "بررسی دعوت‌نامه انجام نشد", requestID)
			return
		}
	default:
		writeError(w, http.StatusUnauthorized, "customer_identity_invalid", "نشست یا دعوت‌نامه معتبر نیست", requestID)
		return
	}

	requestBody, err := json.Marshal(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات ثبت دستگاه Tor معتبر نیست", requestID)
		return
	}
	requestHash := sha256.Sum256(requestBody)
	device, replayed, err := store.EnrollTorClientDevice(
		r.Context(), userID, invitationHash, idempotencyKey, requestHash[:],
		CreateTorClientDeviceInput{
			DeviceRef: input.DeviceRef, DeviceName: input.DeviceName,
			Platform: input.Platform, ClientVersion: input.ClientVersion,
		}, requestID, ip,
	)
	switch {
	case errors.Is(err, ErrDeviceLimitReached):
		writeError(w, http.StatusConflict, "device_limit_reached", "سقف مشترک دستگاه‌های این حساب تکمیل شده است", requestID)
	case errors.Is(err, ErrUserNotActive):
		writeError(w, http.StatusForbidden, "customer_account_inactive", "حساب مشتری فعال نیست", requestID)
	case errors.Is(err, ErrEnrollmentCodeInvalid):
		writeError(w, http.StatusUnauthorized, "enrollment_code_invalid", "دعوت‌نامه Tor معتبر نیست", requestID)
	case errors.Is(err, ErrEnrollmentCodeExpired):
		writeError(w, http.StatusGone, "enrollment_code_expired", "مهلت دعوت‌نامه پایان یافته است", requestID)
	case errors.Is(err, ErrEnrollmentCodeUsed):
		writeError(w, http.StatusConflict, "enrollment_code_used", "این دعوت‌نامه قبلاً مصرف شده است", requestID)
	case errors.Is(err, ErrDeviceConflict):
		writeError(w, http.StatusConflict, "device_conflict", "این دستگاه قبلاً ثبت شده است", requestID)
	case errors.Is(err, ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "کلید درخواست قبلاً با اطلاعات دیگری استفاده شده است", requestID)
	case err != nil:
		a.logger.Error("Tor client enrollment failed", "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "enrollment_failed", "ثبت دستگاه Tor انجام نشد", requestID)
	default:
		if replayed {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(w, http.StatusCreated, device)
	}
}

func (a *API) customerVPNUserFromBearer(w http.ResponseWriter, r *http.Request) (VPNUser, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		writeError(w, http.StatusUnauthorized, "customer_identity_invalid", "نشست مشتری معتبر نیست", requestIDFrom(r))
		return VPNUser{}, false
	}
	claims, externalRef, err := a.verifyCustomerIdentity(r.Context(), authorization)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "customer_identity_invalid", "نشست مشتری معتبر یا تأییدشده نیست", requestIDFrom(r))
		return VPNUser{}, false
	}
	store, ok := a.store.(CustomerSelfServiceStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "حساب مشتری آماده نیست", requestIDFrom(r))
		return VPNUser{}, false
	}
	user, err := store.EnsureExternalVPNUser(r.Context(), externalRef, claims.Name)
	if err != nil {
		a.logger.Error("customer account lookup failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "customer_account_unavailable", "دریافت حساب مشتری انجام نشد", requestIDFrom(r))
		return VPNUser{}, false
	}
	if user.Status != "active" {
		writeError(w, http.StatusForbidden, "customer_account_inactive", "حساب مشتری فعال نیست", requestIDFrom(r))
		return VPNUser{}, false
	}
	return user, true
}

func (a *API) handleTorClientDeviceStatus(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("deviceID")
	if !uuidPattern.MatchString(deviceID) {
		writeError(w, http.StatusBadRequest, "invalid_device_id", "شناسه دستگاه معتبر نیست", requestIDFrom(r))
		return
	}
	user, ok := a.customerVPNUserFromBearer(w, r)
	if !ok {
		return
	}
	store, ok := a.store.(TorClientStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "وضعیت دستگاه Tor آماده نیست", requestIDFrom(r))
		return
	}
	device, err := store.GetTorClientDevice(r.Context(), user.ID, deviceID)
	if errors.Is(err, ErrDeviceNotFound) {
		writeError(w, http.StatusNotFound, "device_not_found", "دستگاه پیدا نشد", requestIDFrom(r))
		return
	}
	if err != nil {
		a.logger.Error("Tor device status lookup failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "device_status_unavailable", "بررسی وضعیت دستگاه انجام نشد", requestIDFrom(r))
		return
	}
	writeJSON(w, http.StatusOK, device)
}

func (a *API) handleTorClientDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("deviceID")
	if !uuidPattern.MatchString(deviceID) {
		writeError(w, http.StatusBadRequest, "invalid_device_id", "شناسه دستگاه معتبر نیست", requestIDFrom(r))
		return
	}
	user, ok := a.customerVPNUserFromBearer(w, r)
	if !ok {
		return
	}
	store, ok := a.store.(TorClientStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "لغو دستگاه Tor آماده نیست", requestIDFrom(r))
		return
	}
	device, err := store.RevokeTorClientDevice(r.Context(), user.ID, deviceID, requestIDFrom(r), remoteIP(r.RemoteAddr))
	if errors.Is(err, ErrDeviceNotFound) {
		writeError(w, http.StatusNotFound, "device_not_found", "دستگاه پیدا نشد", requestIDFrom(r))
		return
	}
	if err != nil {
		a.logger.Error("Tor device revocation failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "device_revoke_failed", "لغو دستگاه Tor انجام نشد", requestIDFrom(r))
		return
	}
	writeJSON(w, http.StatusOK, device)
}
