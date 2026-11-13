package wireguard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type peerBlock struct {
	Start         int
	End           int
	PublicKey     string
	PublicKeyLine int
	AllowedLine   int
	Allowed       []netip.Prefix
}

func validatePublicKey(value string) error {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(raw) != 32 || base64.StdEncoding.EncodeToString(raw) != value {
		return errors.New("WireGuard public key must be canonical base64 encoding of 32 bytes")
	}
	return nil
}

func allowedIPs(peer PeerSpec) string {
	return peer.IPv4.String() + "/32, " + peer.IPv6.String() + "/128"
}

func readSecureConfig(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open WireGuard config without following symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return nil, errors.New("open WireGuard config failed")
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect WireGuard config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("WireGuard config must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("WireGuard config permissions are too broad; expected owner-only access")
	}
	return io.ReadAll(file)
}

func parsePeerBlocks(content []byte) ([]string, []peerBlock, []netip.Addr, error) {
	lines := strings.Split(string(content), "\n")
	blocks := make([]peerBlock, 0)
	interfaceAddresses := make([]netip.Addr, 0)
	currentSection := ""
	interfaceCount := 0
	var current *peerBlock

	finish := func(end int) {
		if current != nil {
			current.End = end
			blocks = append(blocks, *current)
			current = nil
		}
	}

	for index, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			finish(index)
			currentSection = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			switch currentSection {
			case "interface":
				interfaceCount++
				if interfaceCount > 1 {
					return nil, nil, nil, errors.New("multiple WireGuard interface sections are not supported")
				}
			case "peer":
				current = &peerBlock{Start: index, End: len(lines), PublicKeyLine: -1, AllowedLine: -1}
			default:
				return nil, nil, nil, fmt.Errorf("unsupported WireGuard config section %q", currentSection)
			}
			continue
		}

		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			if currentSection == "peer" {
				return nil, nil, nil, fmt.Errorf("invalid setting in peer section at line %d", index+1)
			}
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(strings.SplitN(value, "#", 2)[0])

		if currentSection == "interface" && key == "address" {
			for _, part := range strings.Split(value, ",") {
				prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
				if err != nil {
					return nil, nil, nil, fmt.Errorf("invalid Interface Address at line %d", index+1)
				}
				interfaceAddresses = append(interfaceAddresses, prefix.Addr())
			}
		}

		if currentSection != "peer" || current == nil {
			continue
		}
		switch key {
		case "publickey":
			if current.PublicKeyLine >= 0 {
				return nil, nil, nil, errors.New("peer section contains duplicate PublicKey settings")
			}
			if err := validatePublicKey(value); err != nil {
				return nil, nil, nil, err
			}
			current.PublicKey = value
			current.PublicKeyLine = index
		case "allowedips":
			if current.AllowedLine >= 0 {
				return nil, nil, nil, errors.New("peer section contains duplicate AllowedIPs settings")
			}
			current.AllowedLine = index
			if value == "" {
				return nil, nil, nil, errors.New("peer AllowedIPs setting is empty")
			}
			for _, part := range strings.Split(value, ",") {
				prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
				if err != nil {
					return nil, nil, nil, fmt.Errorf("invalid peer AllowedIPs at line %d", index+1)
				}
				current.Allowed = append(current.Allowed, prefix.Masked())
			}
		}
	}
	finish(len(lines))
	if interfaceCount != 1 {
		return nil, nil, nil, errors.New("WireGuard config must contain exactly one Interface section")
	}
	for _, block := range blocks {
		if block.PublicKey == "" {
			return nil, nil, nil, errors.New("peer section is missing PublicKey")
		}
	}
	return lines, blocks, interfaceAddresses, nil
}

func ensurePeerConfig(content []byte, peer PeerSpec) ([]byte, *peerBlock, bool, error) {
	lines, blocks, interfaceAddresses, err := parsePeerBlocks(content)
	if err != nil {
		return nil, nil, false, err
	}
	for _, address := range interfaceAddresses {
		if address == peer.IPv4 || address == peer.IPv6 {
			return nil, nil, false, errors.New("peer address conflicts with a WireGuard interface address")
		}
	}

	var existing *peerBlock
	for index := range blocks {
		block := &blocks[index]
		if block.PublicKey == peer.PublicKey {
			if existing != nil {
				return nil, nil, false, errors.New("duplicate WireGuard peer public key in config")
			}
			existing = block
			continue
		}
		for _, allowed := range block.Allowed {
			if allowed.Contains(peer.IPv4) || allowed.Contains(peer.IPv6) {
				return nil, nil, false, errors.New("peer address overlaps an existing WireGuard AllowedIPs route")
			}
		}
	}

	desired := allowedIPs(peer)
	if existing != nil {
		prior := *existing
		if len(existing.Allowed) == 2 &&
			existing.Allowed[0] == netip.PrefixFrom(peer.IPv4, 32) &&
			existing.Allowed[1] == netip.PrefixFrom(peer.IPv6, 128) {
			return content, &prior, false, nil
		}
		next := append([]string(nil), lines...)
		replacement := "AllowedIPs = " + desired
		if existing.AllowedLine >= 0 {
			next[existing.AllowedLine] = replacement
		} else {
			next = insertLine(next, existing.PublicKeyLine+1, replacement)
		}
		return []byte(strings.Join(next, "\n")), &prior, true, nil
	}

	next := append([]string(nil), lines...)
	if len(next) > 0 && next[len(next)-1] != "" {
		next = append(next, "")
	}
	next = append(next, "[Peer]", "PublicKey = "+peer.PublicKey, "AllowedIPs = "+desired)
	return []byte(strings.Join(next, "\n")), nil, true, nil
}

func removePeerConfig(content []byte, publicKey string) ([]byte, *peerBlock, bool, error) {
	lines, blocks, _, err := parsePeerBlocks(content)
	if err != nil {
		return nil, nil, false, err
	}
	var target *peerBlock
	for index := range blocks {
		if blocks[index].PublicKey == publicKey {
			if target != nil {
				return nil, nil, false, errors.New("duplicate WireGuard peer public key in config")
			}
			target = &blocks[index]
		}
	}
	if target == nil {
		return content, nil, false, nil
	}
	prior := *target
	next := append([]string(nil), lines[:target.Start]...)
	next = append(next, lines[target.End:]...)
	return []byte(strings.Join(next, "\n")), &prior, true, nil
}

func insertLine(lines []string, index int, value string) []string {
	if index < 0 {
		index = 0
	}
	if index > len(lines) {
		index = len(lines)
	}
	result := make([]string, 0, len(lines)+1)
	result = append(result, lines[:index]...)
	result = append(result, value)
	result = append(result, lines[index:]...)
	return result
}

func atomicWriteConfig(path string, content []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".zeus-wireguard-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	return nil
}
