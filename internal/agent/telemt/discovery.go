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
type DiscoveredUser struct {
	Username          string
	Secret            string
	UserADTag         string
	Enabled           bool
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    uint64
	ExpirationRFC3339 string
	ConnectionLink    string
	TotalOctets       uint64
	CurrentConnections int
	ActiveUniqueIPs   int
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
		du := DiscoveredUser{
			Username:           u.Username,
			Enabled:            u.InRuntime,
			TotalOctets:        u.TotalOctets,
			CurrentConnections: u.CurrentConnections,
			ActiveUniqueIPs:    u.ActiveUniqueIPs,
			ConnectionLink:     preferredConnectionLink(u.Links.TLS, u.Links.Secure, u.Links.Classic),
		}

		// Apply API-returned limits.
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

		// Resolve secret: prefer config file, fall back to link parsing.
		if entry, ok := configSecrets[u.Username]; ok && IsValidSecret(entry.Secret) {
			du.Secret = entry.Secret
			// Config may have more accurate limits than API (e.g. global fallbacks resolved).
			if entry.UserADTag != "" {
				du.UserADTag = entry.UserADTag
			}
		} else {
			du.Secret = extractSecretFromLinks(u.Links)
		}

		result = append(result, du)
	}

	return result, nil
}

// extractSecretFromLinks attempts to extract the raw 32-char hex secret from connection links.
// Priority: classic (raw secret) → secure (dd + secret) → tls (ee + domain + secret).
func extractSecretFromLinks(links UserLinks) string {
	// Classic links contain the raw secret: tg://proxy?...&secret=HEX32
	for _, link := range links.Classic {
		if s := extractSecretParam(link); IsValidSecret(s) {
			return s
		}
	}

	// Secure links: secret param = "dd" + HEX32
	for _, link := range links.Secure {
		s := extractSecretParam(link)
		if strings.HasPrefix(s, "dd") || strings.HasPrefix(s, "DD") {
			raw := s[2:]
			if IsValidSecret(raw) {
				return raw
			}
		}
	}

	// TLS/fake-TLS links: secret param = "ee" + domain_hex + HEX32
	// The raw secret is the last 32 hex chars.
	for _, link := range links.TLS {
		s := extractSecretParam(link)
		if (strings.HasPrefix(s, "ee") || strings.HasPrefix(s, "EE")) && len(s) > 34 {
			raw := s[len(s)-32:]
			if IsValidSecret(raw) {
				// Verify the domain portion is also valid hex.
				domainHex := s[2 : len(s)-32]
				if _, err := hex.DecodeString(domainHex); err == nil {
					return raw
				}
			}
		}
	}

	return ""
}

// extractSecretParam parses a tg:// proxy link and returns the "secret" query parameter.
func extractSecretParam(link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("secret")
}
