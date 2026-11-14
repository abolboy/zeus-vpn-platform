package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      uint32 = 64 * 1024
	argonIterations  uint32 = 3
	argonParallelism uint8  = 2
	argonSaltLength         = 16
	argonKeyLength          = 32
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("unsupported password hash format")
	}
	var memory, iterations uint32
	var parallelism int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, errors.New("invalid Argon2 parameters")
	}
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 8 {
		return false, errors.New("Argon2 parameters out of bounds")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < argonSaltLength || len(salt) > 64 {
		return false, errors.New("invalid password salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != argonKeyLength {
		return false, errors.New("invalid password hash")
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]loginBucket
}

type loginBucket struct {
	start time.Time
	count int
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{entries: make(map[string]loginBucket)}
}

func (l *loginLimiter) allow(key string, now time.Time) bool {
	return l.allowWindow(key, now, 8, 5*time.Minute)
}

func (l *loginLimiter) allowWindow(key string, now time.Time, maximum int, window time.Duration) bool {
	if maximum < 1 || window <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) > 10000 {
		for k, bucket := range l.entries {
			if now.Sub(bucket.start) > 2*window {
				delete(l.entries, k)
			}
		}
	}
	bucket := l.entries[key]
	if bucket.start.IsZero() || now.Sub(bucket.start) >= window || now.Before(bucket.start) {
		l.entries[key] = loginBucket{start: now, count: 1}
		return true
	}
	if bucket.count >= maximum {
		return false
	}
	bucket.count++
	l.entries[key] = bucket
	return true
}

func generateSessionToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256(raw)
	return token, sum[:], nil
}

func parseBearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return "", false
	}
	return token, true
}

func validPasswordForBootstrap(password string) bool {
	return len(password) >= 14 && len(password) <= 1024
}

func parseInteger(value string, fallback, min, max int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min || parsed > max {
		return 0, errors.New("integer out of range")
	}
	return parsed, nil
}
