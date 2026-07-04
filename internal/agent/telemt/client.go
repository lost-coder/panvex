package telemt

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Telemt API endpoint paths and shared log keys. Centralised so the
// per-call sites read as a single token and Sonar S1192 stops firing on
// the duplicates.
const (
	pathHealth                    = "/v1/health"
	pathSecurityPosture           = "/v1/security/posture"
	pathRuntimeGates              = "/v1/runtime/gates"
	pathRuntimeConnectionsSummary = "/v1/runtime/connections/summary"
	pathStatsDcs                  = "/v1/stats/dcs"
	pathStatsUpstreams            = "/v1/stats/upstreams"
	pathStatsSummary              = "/v1/stats/summary"

	logTelemtAPICall = "telemt api call"
)

var (
	// ErrNonLoopbackEndpoint reports a Telemt endpoint outside the local host boundary.
	ErrNonLoopbackEndpoint = errors.New("telemt endpoint must resolve to loopback")
)

// defaultSlowDataTTL bounds staleness for heavier Telemt endpoints while reducing repeated local reads.
const defaultSlowDataTTL = 2 * time.Minute

// defaultRequestTimeout bounds local Telemt API calls to prevent indefinite request hangs.
const defaultRequestTimeout = 15 * time.Second

// defaultFetchRuntimeStateDeadline bounds the total duration of a FetchRuntimeState
// cycle when the caller supplies a context without its own deadline. Without this
// cap, a hung Telemt subsystem could block the snapshot loop for up to
// len(subfetches) * defaultRequestTimeout (~150s) on each cycle. See P2-REL-07.
const defaultFetchRuntimeStateDeadline = 30 * time.Second

// Config contains the local Telemt API location and authorization secret.
type Config struct {
	BaseURL       string
	MetricsURL    string
	Authorization string
}

// Client accesses the Telemt control API through a loopback-only endpoint.
type Client struct {
	baseURL           *url.URL
	metricsURL        *url.URL
	authorization     string
	httpClient        *http.Client
	logger            *slog.Logger
	systemLoadSampler func(context.Context) (RuntimeSystemLoad, error)
	mu                sync.RWMutex
	slowDataTTL       time.Duration
	slowFetchedAt     time.Time
	slowData          slowRuntimeState
	hasSlowData       bool

	// upstreamRate tracks 5-minute upstream connect fail-rate from the
	// Prometheus counters scraped during FetchClientUsageFromMetrics.
	// upstreamCountersMu guards latestUpstreamCounters / hasUpstreamCounters.
	upstreamRate           *UpstreamRateTracker
	upstreamCountersMu     sync.RWMutex
	latestUpstreamCounters UpstreamCounters
	hasUpstreamCounters    bool
}

// InvalidateSlowDataCache forces the next runtime snapshot to refetch slow diagnostics.
func (c *Client) InvalidateSlowDataCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hasSlowData = false
	c.slowFetchedAt = time.Time{}
	c.slowData = slowRuntimeState{}
}

// NewClient validates the target endpoint and constructs a local-only Telemt client.
func NewClient(config Config, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, err
	}

	if !isLoopbackHost(parsed.Hostname()) {
		return nil, ErrNonLoopbackEndpoint
	}

	var metricsURL *url.URL
	if strings.TrimSpace(config.MetricsURL) != "" {
		metricsURL, err = url.Parse(config.MetricsURL)
		if err != nil {
			return nil, err
		}
		if !isLoopbackHost(metricsURL.Hostname()) {
			return nil, ErrNonLoopbackEndpoint
		}
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultRequestTimeout,
		}
	}

	return &Client{
		baseURL:           parsed,
		metricsURL:        metricsURL,
		authorization:     config.Authorization,
		httpClient:        httpClient,
		logger:            slog.Default(),
		systemLoadSampler: collectLocalSystemLoad,
		slowDataTTL:       defaultSlowDataTTL,
		upstreamRate:      NewUpstreamRateTracker(32, 30*time.Second, 6*time.Minute),
	}, nil
}

func isLoopbackHost(host string) bool {
	normalized := strings.TrimSpace(strings.ToLower(host))
	switch normalized {
	case "localhost", "::1":
		return true
	}

	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) getJSONPayload(ctx context.Context, path string) (map[string]any, error) {
	payload := make(map[string]any)
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return nil, err
	}

	return payload, nil
}
