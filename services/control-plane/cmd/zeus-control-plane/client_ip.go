package main

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

// SetTrustedProxyCIDRs enables X-Forwarded-For processing only for requests
// whose immediate TCP peer belongs to an explicitly trusted proxy subnet.
// For the staging ALB, configure only its dedicated public subnet CIDRs.
func (a *API) SetTrustedProxyCIDRs(raw string) error {
	var networks []*net.IPNet
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return errors.New("ZEUS_TRUSTED_PROXY_CIDRS must contain comma-separated CIDRs")
		}
		networks = append(networks, network)
	}
	a.trustedProxyCIDRs = networks
	return nil
}

// clientIP returns the direct peer unless it is inside an explicitly trusted
// proxy CIDR. AWS ALB appends the address it observed to the right side of
// X-Forwarded-For, so the final element is the only element trusted here.
func (a *API) clientIP(r *http.Request) *string {
	peer := remoteIP(r.RemoteAddr)
	if peer == nil || len(a.trustedProxyCIDRs) == 0 {
		return peer
	}
	peerIP := net.ParseIP(*peer)
	if peerIP == nil {
		return peer
	}
	trusted := false
	for _, network := range a.trustedProxyCIDRs {
		if network.Contains(peerIP) {
			trusted = true
			break
		}
	}
	if !trusted {
		return peer
	}

	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return peer
	}
	parts := strings.Split(strings.Join(values, ","), ",")
	for index := len(parts) - 1; index >= 0; index-- {
		candidate := strings.TrimSpace(parts[index])
		if candidate == "" {
			continue
		}
		ip := net.ParseIP(candidate)
		if ip == nil {
			return peer
		}
		value := ip.String()
		return &value
	}
	return peer
}
