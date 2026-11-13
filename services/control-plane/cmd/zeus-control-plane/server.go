package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type API struct {
	version      string
	store        ControlStore
	logger       *slog.Logger
	limiter      *loginLimiter
	hashSlots    chan struct{}
	dummyHash    string
	allowedOrigin map[string]struct{}
}

type contextKey string

const operatorContextKey contextKey = "zeus_operator"
const requestIDContextKey contextKey = "zeus_request_id"

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type sessionResponse struct {
	AccessToken string         `json:"access_token"`
	TokenType   string         `json:"token_type"`
	ExpiresAt   time.Time      `json:"expires_at"`
	Operator    operatorPublic `json:"operator"`
}

type operatorPublic struct {
	ID    string `json:"id"`
	Login string `json:"login"`
	Role  string `json:"role"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type userPage struct {
	Items      []VPNUser `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type cursorPayload struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,128}$`)

func NewAPI(version string, store ControlStore, logger *slog.Logger, allowedOrigins []string) (*API, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var dummyHash string
	if store != nil {
		hash, err := hashPassword("zeus-invalid-account-password")
		if err != nil {
			return nil, fmt.Errorf("initialize authentication verifier: %w", err)
		}
		dummyHash = hash
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return &API{
		version: version, store: store, logger: logger,
		limiter: newLoginLimiter(), hashSlots: make(chan struct{}, 2),
		dummyHash: dummyHash, allowedOrigin: origins,
	}, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /readyz", a.handleReady)
	mux.HandleFunc("GET /v1/meta", a.handleMeta)
	mux.HandleFunc("POST /v1/auth/login", a.handleLogin)
	mux.HandleFunc("GET /v1/auth/me", a.handleMe)
	mux.HandleFunc("POST /v1/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /v1/users", a.handleListUsers)
	mux.HandleFunc("POST /v1/users", a.handleCreateUser)
	mux.HandleFunc("GET /v1/users/{userID}", a.handleGetUser)
	mux.HandleFunc("PATCH /v1/users/{userID}", a.handleUpdateUser)
	return a.middleware(mux)
}

// NewHandler remains as a small compatibility wrapper for health-only tests.
func NewHandler(version string) http.Handler {
	api, err := NewAPI(version, nil, slog.Default(), nil)
	if err != nil {
		panic(err)
	}
	return api.Handler()
}

func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		if origin := r.Header.Get("Origin"); origin != "" {
			if _, ok := a.allowedOrigin[origin]; !ok {
				writeError(w, http.StatusForbidden, "origin_denied", "مبدأ درخواست مجاز نیست", requestID)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		} else if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		defer func() {
			if recovered := recover(); recovered != nil {
				a.logger.Error("request panic recovered", "request_id", requestID)
				writeError(w, http.StatusInternalServerError, "internal_error", "خطای داخلی سرور", requestID)
			}
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *API) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleReady(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "پایگاه داده پیکربندی نشده است", requestIDFrom(r))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		a.logger.Warn("readiness check failed", "request_id", requestIDFrom(r), "error", err)
		writeError(w, http.StatusServiceUnavailable, "not_ready", "سرویس هنوز آماده نیست", requestIDFrom(r))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *API) handleMeta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"product": "ZEUS VPN", "service": "control-plane",
		"apiVersion": "v1", "version": a.version, "stage": "phase2-alpha",
	})
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	requestID := requestIDFrom(r)
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "احراز هویت هنوز پیکربندی نشده است", requestID)
		return
	}
	ip := remoteIP(r.RemoteAddr)
	limiterKey := "unknown"
	if ip != nil {
		limiterKey = *ip
	}
	if !a.limiter.allow(limiterKey, time.Now()) {
		w.Header().Set("Retry-After", "300")
		writeError(w, http.StatusTooManyRequests, "rate_limited", "تعداد تلاش‌ها بیش از حد مجاز است", requestID)
		return
	}

	var input loginRequest
	if err := decodeJSON(w, r, &input, 8*1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "درخواست ورود معتبر نیست", requestID)
		return
	}
	input.Login = strings.TrimSpace(input.Login)
	if input.Login == "" || len(input.Login) > 128 || len(input.Password) == 0 || len(input.Password) > 1024 {
		writeError(w, http.StatusBadRequest, "invalid_request", "درخواست ورود معتبر نیست", requestID)
		return
	}
	select {
	case a.hashSlots <- struct{}{}:
		defer func() { <-a.hashSlots }()
	case <-r.Context().Done():
		return
	default:
		writeError(w, http.StatusTooManyRequests, "authentication_busy", "سامانهٔ ورود شلوغ است؛ دوباره تلاش کنید", requestID)
		return
	}

	operator, err := a.store.FindOperatorByLogin(r.Context(), input.Login)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		a.logger.Error("operator lookup failed", "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "internal_error", "ورود در حال حاضر ممکن نیست", requestID)
		return
	}
	hashToCheck := a.dummyHash
	if err == nil {
		hashToCheck = operator.PasswordHash
	}
	matches, verifyErr := verifyPassword(hashToCheck, input.Password)
	if verifyErr != nil {
		a.logger.Error("stored password hash rejected", "request_id", requestID)
		matches = false
	}
	if err != nil || !matches || operator.Status != "active" {
		a.recordAudit(r, nil, "operator.login", "denied")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "نام کاربری یا گذرواژه نادرست است", requestID)
		return
	}

	token, tokenHash, err := generateSessionToken()
	if err != nil {
		a.logger.Error("session token generation failed", "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "internal_error", "ورود در حال حاضر ممکن نیست", requestID)
		return
	}
	expiresAt := time.Now().UTC().Add(8 * time.Hour)
	if err := a.store.CreateSession(r.Context(), operator.ID, tokenHash, expiresAt); err != nil {
		a.logger.Error("session creation failed", "request_id", requestID)
		writeError(w, http.StatusInternalServerError, "internal_error", "ورود در حال حاضر ممکن نیست", requestID)
		return
	}
	a.recordAudit(r, &operator.ID, "operator.login", "success")
	writeJSON(w, http.StatusOK, sessionResponse{
		AccessToken: token, TokenType: "Bearer", ExpiresAt: expiresAt,
		Operator: operatorPublic{ID: operator.ID, Login: operator.Login, Role: operator.Role},
	})
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]operatorPublic{
		"operator": {ID: operator.ID, Login: operator.Login, Role: operator.Role},
	})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	operator, tokenHash, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	if err := a.store.RevokeSession(r.Context(), tokenHash); err != nil {
		a.logger.Error("session revocation failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "internal_error", "خروج در حال حاضر ممکن نیست", requestIDFrom(r))
		return
	}
	a.recordAudit(r, &operator.ID, "operator.logout", "success")
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request) {
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
	limit, err := parseInteger(r.URL.Query().Get("limit"), 25, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "مقدار صفحه معتبر نیست", requestIDFrom(r))
		return
	}
	var cursor *UserCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "نشانگر صفحه معتبر نیست", requestIDFrom(r))
			return
		}
		var payload cursorPayload
		if json.Unmarshal(decoded, &payload) != nil || payload.CreatedAt.IsZero() || !uuidPattern.MatchString(payload.ID) {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "نشانگر صفحه معتبر نیست", requestIDFrom(r))
			return
		}
		cursor = &UserCursor{CreatedAt: payload.CreatedAt, ID: payload.ID}
	}
	users, err := a.store.ListUsers(r.Context(), limit+1, cursor)
	if err != nil {
		a.logger.Error("user list failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "internal_error", "فهرست کاربران دریافت نشد", requestIDFrom(r))
		return
	}
	page := userPage{Items: users}
	if len(users) > limit {
		last := users[limit-1]
		page.Items = users[:limit]
		payload, _ := json.Marshal(cursorPayload{CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(payload)
	}
	writeJSON(w, http.StatusOK, page)
}


func (a *API) handleGetUser(w http.ResponseWriter, r *http.Request) {
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
	userID := r.PathValue("userID")
	if !uuidPattern.MatchString(userID) {
		writeError(w, http.StatusBadRequest, "invalid_user_id", "شناسه کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	store, ok := a.store.(UserMutationStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ذخیره‌ساز مدیریت کاربران آماده نیست", requestIDFrom(r))
		return
	}
	user, err := store.GetVPNUser(r.Context(), userID)
	if errors.Is(err, ErrVPNUserNotFound) {
		writeError(w, http.StatusNotFound, "user_not_found", "کاربر پیدا نشد", requestIDFrom(r))
		return
	}
	if err != nil {
		a.logger.Error("user lookup failed", "request_id", requestIDFrom(r))
		writeError(w, http.StatusInternalServerError, "internal_error", "دریافت کاربر انجام نشد", requestIDFrom(r))
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	if !canManageUsers(operator.Role) {
		a.recordAudit(r, &operator.ID, "user.create", "denied")
		writeError(w, http.StatusForbidden, "forbidden", "فقط مدیران مجاز به ایجاد کاربر هستند", requestIDFrom(r))
		return
	}
	store, ok := a.store.(UserMutationStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ذخیره‌ساز مدیریت کاربران آماده نیست", requestIDFrom(r))
		return
	}
	var input CreateUserInput
	if err := decodeJSON(w, r, &input, 16*1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if !validDisplayName(input.DisplayName) {
		writeError(w, http.StatusBadRequest, "invalid_display_name", "نام کاربر باید بین ۱ تا ۸۰ نویسه باشد", requestIDFrom(r))
		return
	}
	if input.MaxDevices == 0 {
		input.MaxDevices = 3
	}
	if input.MaxDevices < 1 || input.MaxDevices > 20 {
		writeError(w, http.StatusBadRequest, "invalid_device_limit", "سقف دستگاه باید بین ۱ تا ۲۰ باشد", requestIDFrom(r))
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "کلید یکتای درخواست معتبر نیست", requestIDFrom(r))
		return
	}
	fingerprint, err := json.Marshal(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	hash := sha256.Sum256(fingerprint)
	user, replayed, err := store.CreateVPNUser(r.Context(), operator.ID, "POST:/v1/users", key, hash[:], input, requestIDFrom(r), remoteIP(r.RemoteAddr))
	if errors.Is(err, ErrIdempotencyConflict) {
		writeError(w, http.StatusConflict, "idempotency_conflict", "این کلید قبلاً برای درخواست دیگری استفاده شده است", requestIDFrom(r))
		return
	}
	if err != nil {
		a.logger.Error("user creation failed", "request_id", requestIDFrom(r), "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "ایجاد کاربر انجام نشد", requestIDFrom(r))
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, user)
}

func (a *API) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	if !canManageUsers(operator.Role) {
		a.recordAudit(r, &operator.ID, "user.update", "denied")
		writeError(w, http.StatusForbidden, "forbidden", "فقط مدیران مجاز به ویرایش کاربر هستند", requestIDFrom(r))
		return
	}
	userID := r.PathValue("userID")
	if !uuidPattern.MatchString(userID) {
		writeError(w, http.StatusBadRequest, "invalid_user_id", "شناسه کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	store, ok := a.store.(UserMutationStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ذخیره‌ساز مدیریت کاربران آماده نیست", requestIDFrom(r))
		return
	}
	var input UpdateUserInput
	if err := decodeJSON(w, r, &input, 16*1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات ویرایش کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	if input.DisplayName != nil {
		value := strings.TrimSpace(*input.DisplayName)
		if !validDisplayName(value) {
			writeError(w, http.StatusBadRequest, "invalid_display_name", "نام کاربر باید بین ۱ تا ۸۰ نویسه باشد", requestIDFrom(r))
			return
		}
		input.DisplayName = &value
	}
	if input.Status != nil && *input.Status != "active" && *input.Status != "suspended" && *input.Status != "disabled" {
		writeError(w, http.StatusBadRequest, "invalid_status", "وضعیت کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	if input.MaxDevices != nil && (*input.MaxDevices < 1 || *input.MaxDevices > 20) {
		writeError(w, http.StatusBadRequest, "invalid_device_limit", "سقف دستگاه باید بین ۱ تا ۲۰ باشد", requestIDFrom(r))
		return
	}
	if input.DisplayName == nil && input.Status == nil && input.MaxDevices == nil {
		writeError(w, http.StatusBadRequest, "empty_update", "حداقل یک فیلد برای ویرایش لازم است", requestIDFrom(r))
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "کلید یکتای درخواست معتبر نیست", requestIDFrom(r))
		return
	}
	fingerprint, err := json.Marshal(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات ویرایش کاربر معتبر نیست", requestIDFrom(r))
		return
	}
	hash := sha256.Sum256(fingerprint)
	route := "PATCH:/v1/users/" + userID
	user, replayed, err := store.UpdateVPNUser(r.Context(), operator.ID, userID, route, key, hash[:], input, requestIDFrom(r), remoteIP(r.RemoteAddr))
	if errors.Is(err, ErrIdempotencyConflict) {
		writeError(w, http.StatusConflict, "idempotency_conflict", "این کلید قبلاً برای درخواست دیگری استفاده شده است", requestIDFrom(r))
		return
	}
	if errors.Is(err, ErrVPNUserNotFound) {
		writeError(w, http.StatusNotFound, "user_not_found", "کاربر پیدا نشد", requestIDFrom(r))
		return
	}
	if err != nil {
		a.logger.Error("user update failed", "request_id", requestIDFrom(r), "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "ویرایش کاربر انجام نشد", requestIDFrom(r))
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusOK, user)
}

func canManageUsers(role string) bool {
	return role == "owner" || role == "admin"
}

func validDisplayName(value string) bool {
	if value == "" || len(value) > 320 || utf8.RuneCountInString(value) > 80 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func (a *API) authenticate(w http.ResponseWriter, r *http.Request) (Operator, []byte, bool) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "احراز هویت هنوز پیکربندی نشده است", requestIDFrom(r))
		return Operator{}, nil, false
	}
	token, ok := parseBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "نشست معتبر نیست", requestIDFrom(r))
		return Operator{}, nil, false
	}
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	sum := sha256.Sum256(raw)
	operator, err := a.store.FindOperatorForSession(r.Context(), sum[:])
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			a.logger.Error("session lookup failed", "request_id", requestIDFrom(r))
			writeError(w, http.StatusInternalServerError, "internal_error", "بررسی نشست ممکن نیست", requestIDFrom(r))
			return Operator{}, nil, false
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "نشست منقضی یا لغو شده است", requestIDFrom(r))
		return Operator{}, nil, false
	}
	return operator, sum[:], true
}

func (a *API) recordAudit(r *http.Request, actorID *string, action, outcome string) {
	if a.store == nil {
		return
	}
	if err := a.store.RecordAudit(r.Context(), actorID, nil, action, outcome, requestIDFrom(r), remoteIP(r.RemoteAddr)); err != nil {
		a.logger.Error("audit event could not be persisted", "request_id", requestIDFrom(r), "action", action)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message, requestID string) {
	writeJSON(w, status, errorEnvelope{Error: apiError{Code: code, Message: message, RequestID: requestID}})
}

func newRequestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func requestIDFrom(r *http.Request) string {
	if value, ok := r.Context().Value(requestIDContextKey).(string); ok && value != "" {
		return value
	}
	return "unavailable"
}

func remoteIP(remoteAddress string) *string {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	value := ip.String()
	return &value
}
