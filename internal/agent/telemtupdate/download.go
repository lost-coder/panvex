package telemtupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/updatehosts"
	"golang.org/x/mod/semver"
)

const (
	defaultDownloadTimeout = 5 * time.Minute
	// defaultMaxArchive caps both the downloaded archive (fetchToTemp) and
	// the decompressed member (extractBinary) — spec §10. Telemt binaries
	// are far smaller than this; the cap exists to bound a hostile or
	// compromised release host, not to accommodate legitimate size.
	defaultMaxArchive = 128 << 20 // 128 MiB
	// defaultMaxChecksum bounds the .sha256 sidecar fetch. The file is a
	// single hex digest (64 chars) plus an optional filename; 1 KiB is
	// generous.
	defaultMaxChecksum = 1 << 10
)

var (
	errInsecureScheme  = errors.New("download URL must be https")
	errHostNotAllowed  = errors.New("download URL host is not allowed")
	errArchiveTooLarge = errors.New("download exceeds max archive size")
	// errAlreadyAtTarget is returned by checkDowngrade when target equals
	// the running version. Not a failure: the future Execute state machine
	// (Task 5) treats it as a successful no-op via errors.Is, the same way
	// updater.Execute treats A3 equality as OutcomeNoop.
	errAlreadyAtTarget = errors.New("telemtupdate: already at target version")
)

// Config tunes the download path. Zero values mean "use the default";
// tests inject a custom HTTPClient + AllowedHosts to reach an httptest
// server, and operators can override the host allowlist via env when
// running a private release mirror. Unlike internal/agent/updater's
// Config, there is no AllowInsecure escape hatch: Telemt release assets
// are only ever fetched over https, so tests reach for
// httptest.NewTLSServer instead.
type Config struct {
	HTTPClient   *http.Client
	AllowedHosts []string
	AllowAnyHost bool // set by the "*" sentinel: skip the host allow-list
	MaxArchive   int64
}

// defaultConfig returns the production policy: https-only, allowlist from
// env-or-builtin GitHub hosts, 5-minute total timeout, 128 MiB archive cap.
// Each call constructs a fresh client so tests cannot accidentally share
// state with production.
func defaultConfig() Config {
	p := updatehosts.PolicyFromEnv()
	cfg := Config{
		AllowedHosts: p.Hosts(), // nil when disabled
		AllowAnyHost: p.Disabled(),
		MaxArchive:   defaultMaxArchive,
	}
	cfg.HTTPClient = &http.Client{
		Timeout:       defaultDownloadTimeout,
		CheckRedirect: redirectPolicy(cfg),
	}
	return cfg
}

// redirectPolicy re-validates every redirect hop against the same scheme +
// host rules as the initial URL (via validateDownloadURL), so a 302 from an
// allowed host cannot steer the agent onto an untrusted host. Caps the
// chain at 10 hops, mirroring updater.redirectPolicy.
func redirectPolicy(cfg Config) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return validateDownloadURL(req.URL.String(), cfg)
	}
}

// validateDownloadURL enforces scheme + host policy on a payload URL. The
// agent follows panel-supplied URLs, so this is the choke point that
// prevents a tampered or mistakenly-scoped panel from sending the agent at,
// say, `http://attacker/payload` or `file:///etc/shadow`.
func validateDownloadURL(raw string, cfg Config) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "https" {
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

// fetchToTemp downloads the "<asset>.tar.gz" archive at rawURL into a temp
// file created in destDir (".telemt-update-*"), hashing the bytes as they
// stream so the caller gets a verifiable digest without a second read
// pass. The stream is bounded by cfg.MaxArchive (falling back to
// defaultMaxArchive): a Content-Length above the cap is rejected before
// any bytes are read, and an unbounded/lying body is caught mid-copy.
func fetchToTemp(ctx context.Context, cfg Config, rawURL, destDir string) (tmpPath string, sha256hex string, err error) {
	if err := validateDownloadURL(rawURL, cfg); err != nil {
		return "", "", err
	}
	maxArchive := cfg.MaxArchive
	if maxArchive <= 0 {
		maxArchive = defaultMaxArchive
	}

	resp, err := doGet(ctx, rawURL, cfg)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxArchive {
		return "", "", fmt.Errorf("%w: Content-Length=%d, cap=%d", errArchiveTooLarge, resp.ContentLength, maxArchive)
	}

	tmp, err := os.CreateTemp(destDir, ".telemt-update-archive-*")
	if err != nil {
		return "", "", err
	}
	tmpName := tmp.Name()
	// Cleanup on any error path so a half-written archive does not linger
	// in destDir. Success path ends with the file in place; the caller
	// takes ownership of the path and removes it after extraction.
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	h := sha256.New()
	// Copy at most maxArchive+1 bytes so we can detect overflow.
	limited := io.LimitReader(resp.Body, maxArchive+1)
	written, err := io.Copy(io.MultiWriter(tmp, h), limited)
	if err != nil {
		cleanup()
		return "", "", err
	}
	if written > maxArchive {
		cleanup()
		return "", "", fmt.Errorf("%w: streamed %d bytes, cap=%d", errArchiveTooLarge, written, maxArchive)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", "", err
	}
	return tmpName, hex.EncodeToString(h.Sum(nil)), nil
}

// fetchChecksum downloads the ".tar.gz.sha256" sidecar at rawURL and
// extracts the hex digest via parseChecksumSidecar. The fetch is bounded
// to defaultMaxChecksum: the sidecar is a single digest line, never a
// multi-megabyte payload, so anything larger is treated as malformed
// rather than streamed to completion.
func fetchChecksum(ctx context.Context, cfg Config, rawURL string) (string, error) {
	if err := validateDownloadURL(rawURL, cfg); err != nil {
		return "", err
	}
	resp, err := doGet(ctx, rawURL, cfg)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultMaxChecksum))
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", fmt.Errorf("empty response body")
	}
	sum := parseChecksumSidecar(body)
	if sum == "" {
		return "", fmt.Errorf("checksum sidecar is empty or malformed")
	}
	return sum, nil
}

// parseChecksumSidecar extracts the hex digest from a `.sha256` sidecar.
// CI writes just the digest, but `sha256sum` output ("<hex>  <file>") is
// also tolerated by taking the first whitespace-delimited field. Copied
// from internal/agent/updater.parseChecksumSidecar (unexported there, and
// too small to justify a cross-package export).
func parseChecksumSidecar(b []byte) string {
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// extractBinary scans archivePath for tar entries, requiring exactly one
// regular file member — the Telemt release layout the panel expects
// (upstream names it "telemt", but the name is never checked: writing the
// member into our own os.CreateTemp file in destDir, instead of anywhere
// derived from hdr.Name, is itself the path-traversal defense, so there is
// nothing to gain from also checking the name).
//
// Zero or two-or-more regular members is refused: a repacked or
// README-first archive must not be silently extracted (mirrors
// updater.extractBinaryFromArchive's "wanted entry missing" defense, but
// telemtupdate does not know a specific member name to look for).
//
// Every member's declared size is checked against defaultMaxArchive before
// any bytes are streamed, and the stream itself is bounded to that same
// declared size — together this defends both against a header lying about
// a huge size (rejected up front) and a decompression bomb hidden behind a
// small declared size (the tar reader never yields more than hdr.Size
// bytes for the entry regardless of how much compressed data backs it).
func extractBinary(archivePath, destDir string) (binTmpPath string, size int64, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", 0, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", 0, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	var (
		binPath  string
		binSize  int64
		regCount int
	)

	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			if binPath != "" {
				_ = os.Remove(binPath)
			}
			return "", 0, fmt.Errorf("read tar entry: %w", nextErr)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		regCount++
		if regCount > 1 {
			if binPath != "" {
				_ = os.Remove(binPath)
			}
			return "", 0, fmt.Errorf("archive contains more than one file member")
		}
		if hdr.Size <= 0 || hdr.Size > defaultMaxArchive {
			return "", 0, fmt.Errorf("refusing to extract %q: declared size %d out of bounds (cap %d)", hdr.Name, hdr.Size, int64(defaultMaxArchive))
		}
		path, n, stageErr := stageMember(tr, hdr.Size, destDir)
		if stageErr != nil {
			return "", 0, fmt.Errorf("extract: %w", stageErr)
		}
		binPath, binSize = path, n
	}

	if regCount == 0 {
		return "", 0, fmt.Errorf("archive contains no file member")
	}
	return binPath, binSize, nil
}

// stageMember copies declaredSize bytes of the current tar entry into a new
// executable (0755) temp file created in destDir. destDir must be the
// directory the extracted binary will eventually be swapped into: staging
// next to the destination (instead of the OS temp dir) keeps a later
// same-filesystem os.Rename from failing with EXDEV.
func stageMember(r io.Reader, declaredSize int64, destDir string) (string, int64, error) {
	tmp, err := os.CreateTemp(destDir, ".telemt-update-bin-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temp binary: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	written, err := io.Copy(tmp, io.LimitReader(r, declaredSize+1))
	if err != nil {
		cleanup()
		return "", 0, err
	}
	if written != declaredSize {
		cleanup()
		return "", 0, fmt.Errorf("wrote %d bytes, tar header declares %d", written, declaredSize)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil { //nolint:gosec // G302: the extracted binary must be executable
		cleanup()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", 0, err
	}
	return tmpName, written, nil
}

// canonicalSemver normalises an operator- or panel-supplied version string
// into the leading-"v" form that golang.org/x/mod/semver expects, and
// reports whether the result is a real semver. Copied from
// internal/agent/updater.canonicalSemver (unexported there, and too small
// to justify a cross-package export).
//
// Rules:
//   - "" / "dev" / "snapshot" → not parseable (caller treats as "no
//     provable version", fails closed).
//   - "1.4.7" or "v1.4.7" → "v1.4.7", ok.
//   - "1.4.7-rc1+build" → "v1.4.7-rc1+build", ok (semver handles
//     pre-release ordering correctly: 1.4.7-rc1 < 1.4.7).
//   - "alpha" / "main" / "1.x" → not parseable.
func canonicalSemver(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "dev") || strings.EqualFold(v, "snapshot") {
		return "", false
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return v, true
}

// checkDowngrade is the downgrade gate: fail-closed against a compromised
// panel pinning the agent back to a vulnerable past release, mirroring
// updater.executeWith's gate 1:1 except for the equal-versions branch,
// which returns the typed errAlreadyAtTarget instead of logging and
// returning nil — the future Execute state machine (Task 5) needs to tell
// "already there, converge as a no-op" apart from "gate passed, proceed"
// via errors.Is, since both are non-error outcomes from the caller's
// perspective.
//
// allow=true (Payload.AllowDowngrade) skips every check unconditionally —
// it is the operator's escape hatch for forced reinstall/rollback.
func checkDowngrade(current, target string, allow bool) error {
	if allow {
		return nil
	}
	curr, currOK := canonicalSemver(current)
	next, nextOK := canonicalSemver(target)
	switch {
	case !currOK:
		return fmt.Errorf(
			"refusing update: running version %q is not a parseable semver (set allow_downgrade=true to override)",
			current,
		)
	case !nextOK:
		return fmt.Errorf(
			"refusing update: target version %q is not a parseable semver (set allow_downgrade=true to override)",
			target,
		)
	case semver.Compare(next, curr) == 0:
		return errAlreadyAtTarget
	case semver.Compare(next, curr) < 0:
		return fmt.Errorf(
			"refusing downgrade: target version %q is older than running version %q (set allow_downgrade=true to override)",
			target, current,
		)
	}
	return nil
}
