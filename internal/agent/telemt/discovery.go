package telemt

import (
	"context"
	"encoding/hex"
	"net/url"
	"strings"
)

// SystemInfo holds the response from GET /v1/system/info.
type SystemInfo struct {
	Version    string  `json:"version"`
	ConfigPath string  `json:"config_path"`
	ConfigHash string  `json:"config_hash"`
	Uptime     float64 `json:"uptime_seconds"`
}

// UserInfo represents one user as returned by GET /v1/users.
type UserInfo struct {
	Username           string   `json:"username"`
	InRuntime          bool     `json:"in_runtime"`
	UserADTag          *string  `json:"user_ad_tag"`
	MaxTCPConns        *int     `json:"max_tcp_conns"`
	ExpirationRFC3339  *string  `json:"expiration_rfc3339"`
	DataQuotaBytes     *uint64  `json:"data_quota_bytes"`
	MaxUniqueIPs       *int     `json:"max_unique_ips"`
	CurrentConnections int      `json:"current_connections"`
	ActiveUniqueIPs    int      `json:"active_unique_ips"`
	TotalOctets        uint64   `json:"total_octets"`
	Links              UserLinks `json:"links"`
}

// UserLinks contains connection URIs grouped by type.
type UserLinks struct {
	Classic []string `json:"classic"`
	Secure  []string `json:"secure"`
	TLS     []string `json:"tls"`
}

// DiscoveredUser merges config data (secret, limits) with live stats.
// ConnectionLinks holds every Telemt-returned link (one per
// tls_domain × host); Telemt may emit multiple if the operator
// configured tls_domains.
type DiscoveredUser struct {
	Username           string
	Secret             string
	UserADTag          string
	Enabled            bool
	MaxTCPConns        int
	MaxUniqueIPs       int
	DataQuotaBytes     uint64
	ExpirationRFC3339  string
	ConnectionLinks    []string
	TotalOctets        uint64
	CurrentConnections int
	ActiveUniqueIPs    int
}

// FetchSystemInfo calls GET /v1/system/info and returns parsed metadata.
func (c *Client) FetchSystemInfo(ctx context.Context) (SystemInfo, error) {
	var info SystemInfo
	if err := c.getJSON(ctx, "/v1/system/info", &info); err != nil {
		return SystemInfo{}, err
	}
	return info, nil
}

// FetchUsers calls GET /v1/users and returns all configured users with stats and links.
func (c *Client) FetchUsers(ctx context.Context) ([]UserInfo, error) {
	var users []UserInfo
	if err := c.getJSON(ctx, "/v1/users", &users); err != nil {
		return nil, err
	}
	return users, nil
}

// FetchDiscoveredUsers returns all Telemt users with secrets resolved.
// It tries to read secrets from the config file first (configPath), falling back
// to extracting secrets from connection links if the config is unavailable.
func (c *Client) FetchDiscoveredUsers(ctx context.Context, configPath string) ([]DiscoveredUser, error) {
	users, err := c.FetchUsers(ctx)
	if err != nil {
		return nil, err
	}

	// Try to load secrets from config file.
	var configSecrets map[string]UserEntry
	if configPath != "" {
		cfg, cfgErr := ReadTelemtConfig(configPath)
		if cfgErr == nil {
			configSecrets = cfg.ResolveUsers()
		}
	}

	result := make([]DiscoveredUser, 0, len(users))
	for _, u := range users {
		result = append(result, buildDiscoveredUser(u, configSecrets))
	}

	return result, nil
}

// buildDiscoveredUser merges live UserInfo, API-returned limits, and any
// config-file secrets into a single DiscoveredUser record.
func buildDiscoveredUser(u UserInfo, configSecrets map[string]UserEntry) DiscoveredUser {
	du := DiscoveredUser{
		Username:           u.Username,
		Enabled:            u.InRuntime,
		TotalOctets:        u.TotalOctets,
		CurrentConnections: u.CurrentConnections,
		ActiveUniqueIPs:    u.ActiveUniqueIPs,
		ConnectionLinks:    collectConnectionLinks(u.Links.TLS, u.Links.Secure, u.Links.Classic),
	}
	applyAPIUserLimits(&du, u)
	resolveDiscoveredUserSecret(&du, u, configSecrets)
	return du
}

// applyAPIUserLimits copies optional API-returned limits onto the discovered user.
func applyAPIUserLimits(du *DiscoveredUser, u UserInfo) {
	if u.UserADTag != nil {
		du.UserADTag = *u.UserADTag
	}
	if u.MaxTCPConns != nil {
		du.MaxTCPConns = *u.MaxTCPConns
	}
	if u.MaxUniqueIPs != nil {
		du.MaxUniqueIPs = *u.MaxUniqueIPs
	}
	if u.DataQuotaBytes != nil {
		du.DataQuotaBytes = *u.DataQuotaBytes
	}
	if u.ExpirationRFC3339 != nil {
		du.ExpirationRFC3339 = *u.ExpirationRFC3339
	}
}

// resolveDiscoveredUserSecret prefers the config-file secret when present and
// valid, otherwise falls back to extracting it from the connection link.
func resolveDiscoveredUserSecret(du *DiscoveredUser, u UserInfo, configSecrets map[string]UserEntry) {
	if entry, ok := configSecrets[u.Username]; ok && IsValidSecret(entry.Secret) {
		du.Secret = entry.Secret
		// Config may have more accurate limits than API (e.g. global fallbacks resolved).
		if entry.UserADTag != "" {
			du.UserADTag = entry.UserADTag
		}
		return
	}
	du.Secret = extractSecretFromLinks(u.Links)
}

// extractSecretFromLinks attempts to extract the raw 32-char hex secret from connection links.
// Priority: classic (raw secret) → secure (dd + secret) → tls (ee + domain + secret).
func extractSecretFromLinks(links UserLinks) string {
	if s := secretFromClassicLinks(links.Classic); s != "" {
		return s
	}
	if s := secretFromSecureLinks(links.Secure); s != "" {
		return s
	}
	return secretFromTLSLinks(links.TLS)
}

// secretFromClassicLinks looks for tg://proxy?...&secret=HEX32 links.
func secretFromClassicLinks(classic []string) string {
	for _, link := range classic {
		if s := extractSecretParam(link); IsValidSecret(s) {
			return s
		}
	}
	return ""
}

// secretFromSecureLinks parses "dd" + HEX32 secrets out of secure-mode links.
func secretFromSecureLinks(secure []string) string {
	for _, link := range secure {
		s := extractSecretParam(link)
		if !strings.HasPrefix(s, "dd") && !strings.HasPrefix(s, "DD") {
			continue
		}
		raw := s[2:]
		if IsValidSecret(raw) {
			return raw
		}
	}
	return ""
}

// secretFromTLSLinks parses "ee" + HEX32 + domain_hex secrets from fake-TLS links.
// The raw secret is the first 32 hex chars after the "ee" prefix; everything
// after that is the SNI domain encoded as hex. Earlier versions had this
// reversed which caused every discovered client on a given node to report the
// domain bytes as their secret — triggering spurious
// "same_secret_different_names" conflicts.
func secretFromTLSLinks(tls []string) string {
	for _, link := range tls {
		s := extractSecretParam(link)
		if raw, ok := parseFakeTLSSecret(s); ok {
			return raw
		}
	}
	return ""
}

// parseFakeTLSSecret extracts the raw secret from an "ee"-prefixed secret param.
func parseFakeTLSSecret(s string) (string, bool) {
	if !strings.HasPrefix(s, "ee") && !strings.HasPrefix(s, "EE") {
		return "", false
	}
	if len(s) <= 34 {
		return "", false
	}
	raw := s[2:34]
	if !IsValidSecret(raw) {
		return "", false
	}
	if _, err := hex.DecodeString(s[34:]); err != nil {
		return "", false
	}
	return raw, true
}

// extractSecretParam parses a tg:// proxy link and returns the "secret" query parameter.
func extractSecretParam(link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("secret")
}
