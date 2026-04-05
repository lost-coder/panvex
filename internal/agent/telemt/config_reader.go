package telemt

import (
	"fmt"
	"os"
	"regexp"

	"github.com/BurntSushi/toml"
)

// TelemtConfig represents the full Telemt proxy configuration file (TOML).
// Only fields needed today are mapped; the rest is preserved as raw TOML tables
// so future features can read additional sections without changing this struct.
type TelemtConfig struct {
	General    TelemtGeneralConfig              `toml:"general"`
	Network    TelemtNetworkConfig              `toml:"network"`
	Server     TelemtServerConfig               `toml:"server"`
	Access     TelemtAccessConfig               `toml:"access"`
	Censorship map[string]any                   `toml:"censorship"`
	Timeouts   map[string]any                   `toml:"timeouts"`
	Upstreams  []map[string]any                 `toml:"upstreams"`
	ShowLink   map[string]any                   `toml:"show_link"`
}

// TelemtGeneralConfig holds the [general] section.
type TelemtGeneralConfig struct {
	LogLevel     string `toml:"log_level"`
	Mode         string `toml:"mode"`
	ProxySecrets []string `toml:"proxy_secrets"`
}

// TelemtNetworkConfig holds the [network] section.
type TelemtNetworkConfig struct {
	IPv4      string `toml:"ipv4"`
	IPv6      string `toml:"ipv6"`
	StunURL   string `toml:"stun_url"`
	DNS       string `toml:"dns"`
}

// TelemtServerConfig holds the [server] section.
type TelemtServerConfig struct {
	Port       int    `toml:"port"`
	APIBind    string `toml:"api_bind"`
	TLSDomain  string `toml:"tls_domain"`
}

// TelemtAccessConfig holds the [access] section with user management.
type TelemtAccessConfig struct {
	Users                      map[string]string `toml:"users"`
	UserADTags                 map[string]string `toml:"user_ad_tags"`
	UserMaxTCPConns            map[string]int    `toml:"user_max_tcp_conns"`
	UserMaxTCPConnsGlobalEach  int               `toml:"user_max_tcp_conns_global_each"`
	UserExpirations            map[string]string `toml:"user_expirations"`
	UserDataQuota              map[string]uint64 `toml:"user_data_quota"`
	UserMaxUniqueIPs           map[string]int    `toml:"user_max_unique_ips"`
	UserMaxUniqueIPsGlobalEach int               `toml:"user_max_unique_ips_global_each"`
	ReplayCheckLen             int               `toml:"replay_check_len"`
	ReplayWindowSecs           int               `toml:"replay_window_secs"`
	IgnoreTimeSkew             bool              `toml:"ignore_time_skew"`
}

// UserEntry is a resolved per-user record extracted from multiple access.* maps.
type UserEntry struct {
	Username          string
	Secret            string
	UserADTag         string
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    uint64
	ExpirationRFC3339 string
}

var hexSecretPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// IsValidSecret checks whether s is a valid 32-character hex secret.
func IsValidSecret(s string) bool {
	return hexSecretPattern.MatchString(s)
}

// ReadTelemtConfig reads and parses a Telemt TOML config file.
func ReadTelemtConfig(path string) (TelemtConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TelemtConfig{}, fmt.Errorf("read telemt config: %w", err)
	}

	var cfg TelemtConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return TelemtConfig{}, fmt.Errorf("parse telemt config: %w", err)
	}

	return cfg, nil
}

// ResolveUsers builds a map of username → UserEntry by merging all access.* maps.
func (c *TelemtConfig) ResolveUsers() map[string]UserEntry {
	result := make(map[string]UserEntry, len(c.Access.Users))

	for username, secret := range c.Access.Users {
		entry := UserEntry{
			Username: username,
			Secret:   secret,
		}
		if tag, ok := c.Access.UserADTags[username]; ok {
			entry.UserADTag = tag
		}
		if v, ok := c.Access.UserMaxTCPConns[username]; ok {
			entry.MaxTCPConns = v
		} else if c.Access.UserMaxTCPConnsGlobalEach > 0 {
			entry.MaxTCPConns = c.Access.UserMaxTCPConnsGlobalEach
		}
		if v, ok := c.Access.UserMaxUniqueIPs[username]; ok {
			entry.MaxUniqueIPs = v
		} else if c.Access.UserMaxUniqueIPsGlobalEach > 0 {
			entry.MaxUniqueIPs = c.Access.UserMaxUniqueIPsGlobalEach
		}
		if v, ok := c.Access.UserDataQuota[username]; ok {
			entry.DataQuotaBytes = v
		}
		if v, ok := c.Access.UserExpirations[username]; ok {
			entry.ExpirationRFC3339 = v
		}

		result[username] = entry
	}

	return result
}
