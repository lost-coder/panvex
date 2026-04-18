package clients

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Validation sentinels. Callers match with errors.Is.
var (
	ErrNameRequired    = errors.New("client name is required")
	ErrUserADTag       = errors.New("user_ad_tag must contain exactly 32 hex characters")
	ErrExpiration      = errors.New("expiration_rfc3339 must be a valid RFC3339 timestamp")
	ErrTargetsRequired = errors.New("client must target at least one agent")
	ErrInvalidSecret   = errors.New("invalid secret format: must be 32 hex characters")
)

var hexSecret32 = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// IsValidHexSecret reports whether s is a 32-char lowercase- or
// uppercase-hex string (the MTProto secret format used by Telemt).
func IsValidHexSecret(s string) bool {
	return hexSecret32.MatchString(s)
}

// RandomHexString returns 2*size lowercase hex characters sourced from
// crypto/rand. Used for secrets and user_ad_tag values.
func RandomHexString(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// ResolveUserADTag validates and normalizes a user_ad_tag value. Empty
// input falls back to `fallback` when non-empty, otherwise a fresh
// random tag is minted. Non-empty input must be exactly 32 hex chars;
// it is returned lowercase. Any other input yields ErrUserADTag.
func ResolveUserADTag(value string, fallback string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if fallback != "" {
			return fallback, nil
		}
		return RandomHexString(16)
	}
	if len(trimmed) != 32 {
		return "", ErrUserADTag
	}
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", ErrUserADTag
	}
	return strings.ToLower(trimmed), nil
}

// NormalizeExpiration validates and returns a UTC-normalized RFC3339
// timestamp, or the empty string when the input is empty. Invalid input
// yields ErrExpiration.
func NormalizeExpiration(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return "", ErrExpiration
	}
	return parsed.UTC().Format(time.RFC3339), nil
}

// NormalizedIDs trims, de-duplicates, and sorts a slice of ID strings.
// Empty strings are dropped. Used for FleetGroupIDs / AgentIDs in
// client mutation inputs.
func NormalizedIDs(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		unique[trimmed] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
