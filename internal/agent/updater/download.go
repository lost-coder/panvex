package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/updatehosts"
)

// EnvAllowedHosts is re-exported from updatehosts so existing agent call
// sites and tests keep a stable reference to the env var name.
const EnvAllowedHosts = updatehosts.EnvAllowedHosts

const (
	defaultDownloadTimeout = 5 * time.Minute
	defaultMaxArchive      = 256 << 20 // 256 MB
)

var (
	errInsecureScheme  = errors.New("download URL must be https")
	errHostNotAllowed  = errors.New("download URL host is not allowed")
	errArchiveTooLarge = errors.New("download exceeds max archive size")
)

// Config tunes the download path. Zero values mean "use the default";
// tests inject a custom HTTPClient + AllowedHosts to reach an httptest
// server, and operators can override the host allowlist via env when
// running a private release mirror.
type Config struct {
	HTTPClient    *http.Client
	AllowedHosts  []string
	AllowInsecure bool
	AllowAnyHost  bool // set by the "*" sentinel: skip the host allow-list
	MaxArchive    int64
}

// defaultConfig returns the production policy: HTTPS-only, allowlist
// from env-or-builtin GitHub hosts, 5-minute total timeout, 256 MB
// archive cap. Each call constructs a fresh client so tests cannot
// accidentally share state with production.
func defaultConfig() Config {
	p := updatehosts.PolicyFromEnv()
	return Config{
		HTTPClient:   &http.Client{Timeout: defaultDownloadTimeout},
		AllowedHosts: p.Hosts(), // nil when disabled
		AllowAnyHost: p.Disabled(),
		MaxArchive:   defaultMaxArchive,
	}
}

// validateDownloadURL enforces scheme + host policy on a payload URL.
// The agent follows panel-supplied URLs, so this is the choke point
// that prevents a tampered or mistakenly-scoped panel from sending the
// agent at, say, `http://attacker/payload` or `file:///etc/shadow`.
func validateDownloadURL(raw string, cfg Config) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	// HTTPS is always fine; plain HTTP is only allowed when the operator
	// has explicitly opted in (loopback / airgap testing). Anything else
	// is an immediate reject.
	if scheme != "https" && (!cfg.AllowInsecure || scheme != "http") {
		return fmt.Errorf("%w: scheme=%q", errInsecureScheme, scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url has no host: %q", raw)
	}
	if cfg.AllowAnyHost {
		return nil
	}
	if !hostMatchesAllowlist(host, cfg.AllowedHosts) {
		return fmt.Errorf("%w: host=%q", errHostNotAllowed, host)
	}
	return nil
}

func hostMatchesAllowlist(host string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(host, a) {
			return true
		}
	}
	return false
}

// downloadBytes fetches url and returns its body, bounded to maxBytes.
// Used for small companion files (the checksum sidecar) where streaming
// to disk is unnecessary.
func downloadBytes(ctx context.Context, rawURL string, maxBytes int64, cfg Config) ([]byte, error) {
	if err := validateDownloadURL(rawURL, cfg); err != nil {
		return nil, err
	}
	resp, err := doGet(ctx, rawURL, cfg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	return body, nil
}

// downloadToTemp streams an archive to a temp file with a hard size
// cap. Pre-stream rejection happens when the server advertises a
// Content-Length over the cap; otherwise an io.LimitReader catches
// overflow during the copy and we surface a typed error.
func downloadToTemp(ctx context.Context, rawURL string, cfg Config) (string, error) {
	if err := validateDownloadURL(rawURL, cfg); err != nil {
		return "", err
	}
	maxArchive := cfg.MaxArchive
	if maxArchive <= 0 {
		maxArchive = defaultMaxArchive
	}

	resp, err := doGet(ctx, rawURL, cfg)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxArchive {
		return "", fmt.Errorf("%w: Content-Length=%d, cap=%d", errArchiveTooLarge, resp.ContentLength, maxArchive)
	}

	tmp, err := os.CreateTemp("", "panvex-agent-update-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	// Cleanup on any error path so a half-written archive does not
	// linger in /tmp. Success path ends with the file in place; the
	// caller takes ownership of the path and deletes it after extract.
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	// Copy at most maxArchive+1 bytes so we can detect overflow.
	limited := io.LimitReader(resp.Body, maxArchive+1)
	written, err := io.Copy(tmp, limited)
	if err != nil {
		cleanup()
		return "", err
	}
	if written > maxArchive {
		cleanup()
		return "", fmt.Errorf("%w: streamed %d bytes, cap=%d", errArchiveTooLarge, written, maxArchive)
	}

	if err := os.Chmod(tmpName, 0o700); err != nil { //nolint:gosec // G302: 0o700 keeps the binary owner-only but still needs +x to run
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}

func doGet(ctx context.Context, rawURL string, cfg Config) (*http.Response, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultDownloadTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp, nil
}
