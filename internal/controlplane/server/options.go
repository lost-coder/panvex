package server

import (
	"io/fs"
	"log/slog"
	"net"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// R-Q-01/07: runtime dependencies bundle (Options) extracted from
// server.go so the Server struct stays focused on internal shape;
// callers only see the public knobs in this file.

// Options defines the runtime dependencies used by the control-plane server.
type Options struct {
	Now            func() time.Time
	Users          []auth.User
	Store          storage.Store
	UIFiles        fs.FS
	PanelRuntime   PanelRuntime
	RequestRestart func() error
	// TrustedProxyCIDRs lists additional CIDR ranges whose X-Forwarded-For
	// header is trusted for rate-limit key extraction. Loopback addresses
	// are always trusted regardless of this setting.
	//
	// WARNING: When the control-plane runs behind a non-loopback reverse
	// proxy and this list is empty, every inbound request resolves to the
	// proxy's IP for rate-limit keying. All clients then share a single
	// bucket and will be throttled collectively. Always configure this
	// field to include every intermediate proxy/load-balancer CIDR.
	TrustedProxyCIDRs []*net.IPNet
	// EncryptionKey, when set, encrypts the CA private key at rest using
	// AES-256-GCM. The key is derived from this passphrase via SHA-256.
	// Existing unencrypted keys are transparently migrated on next save.
	EncryptionKey string
	// Logger is the structured logger for the server. If nil, slog.Default() is used.
	Logger *slog.Logger
	// Version is the panel version string (e.g. "v1.2.3" or "dev").
	Version string
	// CommitSHA is the git commit hash baked in at build time.
	CommitSHA string
	// BuildTime is the RFC3339 build timestamp baked in at build time.
	BuildTime string
	// MetricsScrapeToken, when non-empty, enables the GET /metrics endpoint
	// and requires callers to present `Authorization: Bearer <token>` with a
	// byte-for-byte match. When empty, the /metrics route is not registered
	// at all (silent opt-in) so production deployments that never set the env
	// var cannot accidentally expose runtime telemetry.
	MetricsScrapeToken string
	// Intervals overrides the default worker / poller cadences. Zero-valued
	// fields fall back to DefaultIntervals(). Tests use this to compress
	// retention sweeps and rollup scans into milliseconds.
	Intervals Intervals
	// LoginTimingFloor sets the wall-clock minimum every login response
	// (success or failure) is padded to (R-S-19). Zero (unset) falls
	// back to the production default of 150ms. Pass any negative value
	// to disable the pad entirely — tests use this to avoid burning
	// real wall-clock seconds in the suite.
	LoginTimingFloor time.Duration
}
