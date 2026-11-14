package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/netip"
	"net/http"
	"regexp"
	"strings"
)

var (
	nodeInterfaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)
	nodeDNSNamePattern = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))*$`)
)

func (a *API) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	operator, _, ok := a.authenticate(w, r)
	if !ok {
		return
	}
	if !canManageUsers(operator.Role) {
		a.recordAudit(r, &operator.ID, "node.create", "denied")
		writeError(w, http.StatusForbidden, "forbidden", "فقط مدیران مجاز به ثبت نود هستند", requestIDFrom(r))
		return
	}
	var input CreateVPNNodeInput
	if err := decodeJSON(w, r, &input, 16*1024); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات نود معتبر نیست", requestIDFrom(r))
		return
	}
	normalized, err := validateCreateVPNNodeInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_node_config", err.Error(), requestIDFrom(r))
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !idempotencyKeyPattern.MatchString(key) {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "کلید یکتای درخواست معتبر نیست", requestIDFrom(r))
		return
	}
	requestBody, err := json.Marshal(normalized)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "اطلاعات نود معتبر نیست", requestIDFrom(r))
		return
	}
	requestHash := sha256.Sum256(requestBody)
	store, ok := a.store.(NodeMutationStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "ذخیره‌ساز نودها آماده نیست", requestIDFrom(r))
		return
	}
	node, replayed, err := store.CreateVPNNode(
		r.Context(), operator.ID, "POST:/v1/nodes", key, requestHash[:],
		normalized, requestIDFrom(r), remoteIP(r.RemoteAddr),
	)
	switch {
	case errors.Is(err, ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "این کلید قبلاً برای درخواست دیگری استفاده شده است", requestIDFrom(r))
		return
	case errors.Is(err, ErrNodeConflict):
		writeError(w, http.StatusConflict, "node_name_conflict", "نودی با این نام قبلاً ثبت شده است", requestIDFrom(r))
		return
	case err != nil:
		a.logger.Error("node creation failed", "request_id", requestIDFrom(r), "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "ثبت نود انجام نشد", requestIDFrom(r))
		return
	}
	if replayed {
		w.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(w, http.StatusCreated, node)
}

func validateCreateVPNNodeInput(input CreateVPNNodeInput) (CreateVPNNodeInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Region = strings.TrimSpace(input.Region)
	input.EndpointHost = strings.TrimSpace(input.EndpointHost)
	input.PublicKey = strings.TrimSpace(input.PublicKey)
	input.InterfaceName = strings.TrimSpace(input.InterfaceName)
	input.ClientIPv4Pool = strings.TrimSpace(input.ClientIPv4Pool)
	input.ClientIPv6Pool = strings.TrimSpace(input.ClientIPv6Pool)
	input.InterfaceIPv4 = strings.TrimSpace(input.InterfaceIPv4)
	input.InterfaceIPv6 = strings.TrimSpace(input.InterfaceIPv6)

	if !validDisplayName(input.Name) || !validDisplayName(input.Region) {
		return CreateVPNNodeInput{}, errors.New("نام نود و منطقه باید بین ۱ تا ۸۰ نویسه معتبر باشند")
	}
	if input.EndpointPort < 1 || input.EndpointPort > 65535 {
		return CreateVPNNodeInput{}, errors.New("پورت Endpoint باید بین ۱ تا ۶۵۵۳۵ باشد")
	}
	if input.EndpointHost == "" || strings.ContainsAny(input.EndpointHost, "/\\?#@ \t\r\n") {
		return CreateVPNNodeInput{}, errors.New("Endpoint باید فقط IP یا نام میزبان باشد، نه URL یا مسیر")
	}
	if address, err := netip.ParseAddr(input.EndpointHost); err == nil {
		if address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() {
			return CreateVPNNodeInput{}, errors.New("آدرس IP مربوط به Endpoint معتبر نیست")
		}
		input.EndpointHost = address.String()
	} else if !nodeDNSNamePattern.MatchString(input.EndpointHost) {
		return CreateVPNNodeInput{}, errors.New("نام میزبان Endpoint معتبر نیست")
	} else {
		input.EndpointHost = strings.ToLower(input.EndpointHost)
	}
	if !validWireGuardPublicKey(input.PublicKey) {
		return CreateVPNNodeInput{}, errors.New("کلید عمومی WireGuard باید Base64 معتبر با طول ۳۲ بایت باشد")
	}
	if !nodeInterfaceNamePattern.MatchString(input.InterfaceName) {
		return CreateVPNNodeInput{}, errors.New("نام اینترفیس لینوکس معتبر نیست؛ حداکثر ۱۵ نویسه")
	}

	ipv4Pool, err := netip.ParsePrefix(input.ClientIPv4Pool)
	if err != nil || !ipv4Pool.Addr().Is4() || ipv4Pool.Bits() < 16 || ipv4Pool.Bits() > 30 {
		return CreateVPNNodeInput{}, errors.New("IPv4 pool باید CIDR معتبر با پیشوند بین /16 تا /30 باشد")
	}
	ipv6Pool, err := netip.ParsePrefix(input.ClientIPv6Pool)
	if err != nil || !ipv6Pool.Addr().Is6() || ipv6Pool.Bits() < 48 || ipv6Pool.Bits() > 120 {
		return CreateVPNNodeInput{}, errors.New("IPv6 pool باید CIDR معتبر با پیشوند بین /48 تا /120 باشد")
	}
	ipv4Pool = ipv4Pool.Masked()
	ipv6Pool = ipv6Pool.Masked()

	ipv4, err := netip.ParseAddr(input.InterfaceIPv4)
	if err != nil || !ipv4.Is4() || !ipv4.IsGlobalUnicast() || !ipv4Pool.Contains(ipv4) ||
		ipv4 == ipv4Pool.Addr() || isIPv4Broadcast(ipv4, ipv4Pool) {
		return CreateVPNNodeInput{}, errors.New("IPv4 اینترفیس باید آدرس میزبان معتبر داخل IPv4 pool باشد")
	}
	ipv6, err := netip.ParseAddr(input.InterfaceIPv6)
	if err != nil || !ipv6.Is6() || ipv6.Is4In6() || !ipv6.IsGlobalUnicast() ||
		!ipv6Pool.Contains(ipv6) || ipv6 == ipv6Pool.Addr() {
		return CreateVPNNodeInput{}, errors.New("IPv6 اینترفیس باید آدرس میزبان معتبر داخل IPv6 pool باشد")
	}

	input.ClientIPv4Pool = ipv4Pool.String()
	input.ClientIPv6Pool = ipv6Pool.String()
	input.InterfaceIPv4 = ipv4.String()
	input.InterfaceIPv6 = ipv6.String()
	return input, nil
}
