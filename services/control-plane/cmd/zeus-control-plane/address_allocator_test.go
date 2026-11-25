package main

import (
	"errors"
	"net/netip"
	"testing"
)

func TestAllocateNodeAddressFindsIPv6AfterMoreThan65536Used(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8::/48")
	used := make(map[netip.Addr]struct{}, 65536)
	candidate := prefix.Masked().Addr().Next()
	for i := 0; i < 65536; i++ {
		used[candidate] = struct{}{}
		candidate = candidate.Next()
	}
	got, err := allocateNodeAddress(prefix, used, false)
	if err != nil {
		t.Fatalf("allocateNodeAddress() error = %v", err)
	}
	if got != candidate {
		t.Fatalf("allocateNodeAddress() = %s, want %s", got, candidate)
	}
}

func TestAllocateNodeAddressReturnsExhaustedForIPv6Slash120(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8::/120")
	used := make(map[netip.Addr]struct{}, 255)
	for candidate := prefix.Masked().Addr().Next(); prefix.Contains(candidate); candidate = candidate.Next() {
		used[candidate] = struct{}{}
	}
	if _, err := allocateNodeAddress(prefix, used, false); !errors.Is(err, ErrAddressPoolExhausted) {
		t.Fatalf("allocateNodeAddress() error = %v, want %v", err, ErrAddressPoolExhausted)
	}
}

func TestAllocateNodeAddressRejectsInvalidPools(t *testing.T) {
	tests := []struct {
		name   string
		prefix netip.Prefix
		ipv4   bool
	}{
		{name: "invalid prefix", prefix: netip.Prefix{}, ipv4: false},
		{name: "IPv4 passed as IPv6", prefix: netip.MustParsePrefix("10.0.0.0/16"), ipv4: false},
		{name: "IPv6 passed as IPv4", prefix: netip.MustParsePrefix("2001:db8::/48"), ipv4: true},
		{name: "IPv6 link-local", prefix: netip.MustParsePrefix("fe80::/48"), ipv4: false},
		{name: "IPv6 multicast", prefix: netip.MustParsePrefix("ff02::/48"), ipv4: false},
		{name: "IPv4-mapped IPv6", prefix: netip.MustParsePrefix("::ffff:192.0.2.0/120"), ipv4: false},
		{name: "IPv4 link-local", prefix: netip.MustParsePrefix("169.254.0.0/16"), ipv4: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := allocateNodeAddress(tt.prefix, nil, tt.ipv4); !errors.Is(err, ErrAddressPoolExhausted) {
				t.Fatalf("allocateNodeAddress() error = %v, want %v", err, ErrAddressPoolExhausted)
			}
		})
	}
}

func TestAllocateNodeAddressAllowsIPv6ULA(t *testing.T) {
	got, err := allocateNodeAddress(netip.MustParsePrefix("fd00::/48"), nil, false)
	if err != nil {
		t.Fatalf("allocateNodeAddress() error = %v", err)
	}
	if want := netip.MustParseAddr("fd00::1"); got != want {
		t.Fatalf("allocateNodeAddress() = %s, want %s", got, want)
	}
}

func TestAllocateNodeAddressIPv4SkipsNetworkAndBroadcast(t *testing.T) {
	prefix := netip.MustParsePrefix("10.0.0.0/30")
	got, err := allocateNodeAddress(prefix, nil, true)
	if err != nil {
		t.Fatalf("allocateNodeAddress() error = %v", err)
	}
	if want := netip.MustParseAddr("10.0.0.1"); got != want {
		t.Fatalf("allocateNodeAddress() = %s, want %s", got, want)
	}
	used := map[netip.Addr]struct{}{
		netip.MustParseAddr("10.0.0.1"): {},
		netip.MustParseAddr("10.0.0.2"): {},
	}
	if _, err := allocateNodeAddress(prefix, used, true); !errors.Is(err, ErrAddressPoolExhausted) {
		t.Fatalf("allocateNodeAddress() error = %v, want %v", err, ErrAddressPoolExhausted)
	}
}

func TestAllocateNodeAddressIgnoresUsedAddressesOutsidePrefix(t *testing.T) {
	prefix := netip.MustParsePrefix("2001:db8::/120")
	used := map[netip.Addr]struct{}{
		netip.MustParseAddr("2001:db8:1::1"): {},
	}
	got, err := allocateNodeAddress(prefix, used, false)
	if err != nil {
		t.Fatalf("allocateNodeAddress() error = %v", err)
	}
	if want := netip.MustParseAddr("2001:db8::1"); got != want {
		t.Fatalf("allocateNodeAddress() = %s, want %s", got, want)
	}
}
