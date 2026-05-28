package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// StoreReader is the minimum surface OperationalStore needs from
// persistence. The control-plane wires this onto its sqlc-generated
// store; tests pass a hand-rolled fake.
type StoreReader interface {
	ReadPanelColumn(ctx context.Context, col string) (raw string, err error)
	ReadRuntimeSetting(ctx context.Context, name string) (valueJSON string, updatedAt time.Time, updatedBy string, err error)
}

// StoreWriter is the write side; populated in a later task.
type StoreWriter interface {
	WritePanelColumn(ctx context.Context, col, raw string, who string) error
	WriteRuntimeSetting(ctx context.Context, name, valueJSON, who string) error
}

// snapshot is the immutable view returned by getters.
type snapshot struct {
	values     map[string]string // setting name -> raw scalar value (JSON for json type)
	updatedAt  map[string]time.Time
	updatedBy  map[string]string
	sources    map[string]Source // setting name -> resolved source
	overridden map[string]bool   // setting name -> true when env override applied
}

// OperationalStore exposes typed getters and (later) batch writers
// over the operational registry.
type OperationalStore struct {
	reader StoreReader
	writer StoreWriter // optional; used by Phase 7 PUT handler

	env map[string]string // optional read-time env overrides; nil disables

	cache atomic.Pointer[snapshot]
}

// UseEnv enables read-time env overrides from the given environment
// ("KEY=VALUE", e.g. os.Environ()). Call once during wiring; nil/unset
// disables env override.
func (s *OperationalStore) UseEnv(environ []string) { s.env = envSliceToMap(environ) }

// NewOperationalStore wraps a reader; pass NewOperationalStoreRW when
// writes are also needed.
func NewOperationalStore(r StoreReader) *OperationalStore {
	s := &OperationalStore{reader: r}
	s.cache.Store(&snapshot{values: map[string]string{}})
	return s
}

// NewOperationalStoreRW wraps both reader and writer.
func NewOperationalStoreRW(r StoreReader, w StoreWriter) *OperationalStore {
	s := NewOperationalStore(r)
	s.writer = w
	return s
}

// Reload rebuilds the in-memory snapshot from the underlying store.
func (s *OperationalStore) Reload(ctx context.Context) error {
	values := map[string]string{}
	updated := map[string]time.Time{}
	by := map[string]string{}
	sources := map[string]Source{}
	overridden := map[string]bool{}

	for _, f := range AllFields() {
		if f.Class != ClassOperational {
			continue
		}
		raw, ok, err := s.read(ctx, f)
		if err != nil {
			return fmt.Errorf("settings: reload %s: %w", f.Name, err)
		}
		switch {
		case ok:
			values[f.Name] = raw
			sources[f.Name] = SourceDB
		case f.HasDefault:
			values[f.Name] = f.Default
			sources[f.Name] = SourceDefault
		default:
			values[f.Name] = ""
			sources[f.Name] = SourceDefault
		}
		if envVal, hit := envOverrideValue(f, s.env); hit {
			values[f.Name] = envVal
			sources[f.Name] = SourceEnv
			overridden[f.Name] = true
		}
	}
	s.cache.Store(&snapshot{
		values:     values,
		updatedAt:  updated,
		updatedBy:  by,
		sources:    sources,
		overridden: overridden,
	})
	return nil
}

func (s *OperationalStore) read(ctx context.Context, f FieldMeta) (string, bool, error) {
	if strings.HasPrefix(f.Store, "panel_settings.") {
		col := strings.TrimPrefix(f.Store, "panel_settings.")
		raw, err := s.reader.ReadPanelColumn(ctx, col)
		if err != nil {
			return "", false, nil // not-set is not fatal at load time
		}
		return raw, raw != "", nil
	}
	if f.Store == "runtime_settings" {
		jsonText, _, _, err := s.reader.ReadRuntimeSetting(ctx, f.Name)
		if err != nil {
			return "", false, nil
		}
		decoded, err := decodeRuntimeJSON(f.Type, jsonText)
		if err != nil {
			return "", false, err
		}
		return decoded, true, nil
	}
	return "", false, fmt.Errorf("settings: unknown store %q on %s", f.Store, f.Name)
}

func decodeRuntimeJSON(t Type, body string) (string, error) {
	body = strings.TrimSpace(body)
	switch t {
	case TypeString, TypeEnum, TypeURL, TypeHostPort:
		var s string
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			return "", err
		}
		return s, nil
	case TypeInt:
		var n int64
		if err := json.Unmarshal([]byte(body), &n); err != nil {
			return "", err
		}
		return strconv.FormatInt(n, 10), nil
	case TypeBool:
		var b bool
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case TypeDuration:
		var s string
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			return "", err
		}
		return s, nil
	case TypeJSON:
		return body, nil
	}
	return "", fmt.Errorf("settings: decodeRuntimeJSON: unsupported type %s", t)
}

// rawByName fetches the cached raw value or empty string.
func (s *OperationalStore) rawByName(name string) string {
	snap := s.cache.Load()
	if snap == nil {
		return ""
	}
	return snap.values[name]
}

// RawByName exposes the cached raw scalar for HTTP rendering.
// Returns "" when the store is empty.
func (s *OperationalStore) RawByName(name string) string {
	return s.rawByName(name)
}

// Source reports the resolved provenance of the named setting in the
// current snapshot. Unknown / unloaded fields report SourceDefault.
func (s *OperationalStore) Source(name string) Source {
	snap := s.cache.Load()
	if snap == nil || snap.sources == nil {
		return SourceDefault
	}
	if src, ok := snap.sources[name]; ok {
		return src
	}
	return SourceDefault
}

// OverriddenByEnv reports whether the named setting's value in the
// current snapshot comes from a read-time env override.
func (s *OperationalStore) OverriddenByEnv(name string) bool {
	snap := s.cache.Load()
	if snap == nil || snap.overridden == nil {
		return false
	}
	return snap.overridden[name]
}

// --- typed getters (one per operational field) ---

func (s *OperationalStore) HTTPPublicURL() string      { return s.rawByName("http.public_url") }
func (s *OperationalStore) GRPCPublicEndpoint() string { return s.rawByName("grpc.public_endpoint") }

func (s *OperationalStore) PasswordMinLength() int {
	n, _ := strconv.Atoi(s.rawByName("auth.password_min_length"))
	if n == 0 {
		return 10
	}
	return n
}

func (s *OperationalStore) RetentionJSON() string { return s.rawByName("retention") }
func (s *OperationalStore) GeoIPJSON() string     { return s.rawByName("geoip") }

func (s *OperationalStore) UpdatesChannel() string {
	v := s.rawByName("updates.channel")
	if v == "" {
		return "stable"
	}
	return v
}
func (s *OperationalStore) UpdatesAllowPrerelease() bool {
	b, _ := strconv.ParseBool(s.rawByName("updates.allow_prerelease"))
	return b
}

// --- duration/int helpers (fall back to registry default on miss or parse error) ---

func (s *OperationalStore) durationByName(name string) time.Duration {
	if raw := s.rawByName(name); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			return d
		}
	}
	for _, f := range AllFields() {
		if f.Name == name && f.HasDefault {
			d, _ := time.ParseDuration(f.Default)
			return d
		}
	}
	return 0
}

func (s *OperationalStore) intByName(name string) int {
	if raw := s.rawByName(name); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
	}
	for _, f := range AllFields() {
		if f.Name == name && f.HasDefault {
			n, _ := strconv.Atoi(f.Default)
			return n
		}
	}
	return 0
}

// --- typed getters for audited operational fields ---

func (s *OperationalStore) AgentsOutboundBackoffInitial() time.Duration {
	return s.durationByName("agents.outbound_backoff_initial")
}
func (s *OperationalStore) AgentsOutboundBackoffMax() time.Duration {
	return s.durationByName("agents.outbound_backoff_max")
}
func (s *OperationalStore) AgentsPresenceDegradedAfter() time.Duration {
	return s.durationByName("agents.presence_degraded_after")
}
func (s *OperationalStore) AgentsPresenceOfflineAfter() time.Duration {
	return s.durationByName("agents.presence_offline_after")
}

func (s *OperationalStore) AuthPasswordLockoutDuration() time.Duration {
	return s.durationByName("auth.password_lockout_duration")
}
func (s *OperationalStore) AuthPasswordLockoutMaxAttempts() int {
	return s.intByName("auth.password_lockout_max_attempts")
}
func (s *OperationalStore) AuthSessionIdleTimeout() time.Duration {
	return s.durationByName("auth.session_idle_timeout")
}
func (s *OperationalStore) AuthSessionMaxLifetime() time.Duration {
	return s.durationByName("auth.session_max_lifetime")
}
func (s *OperationalStore) AuthTOTPLockoutDuration() time.Duration {
	return s.durationByName("auth.totp_lockout_duration")
}
func (s *OperationalStore) AuthTOTPSetupTTL() time.Duration {
	return s.durationByName("auth.totp_setup_ttl")
}

func (s *OperationalStore) JobsAckExpiryInterval() time.Duration {
	return s.durationByName("jobs.ack_expiry_interval")
}
func (s *OperationalStore) JobsAckExpiryTTL() time.Duration {
	return s.durationByName("jobs.ack_expiry_ttl")
}
func (s *OperationalStore) JobsClientJobTTL() time.Duration {
	return s.durationByName("jobs.client_job_ttl")
}
func (s *OperationalStore) JobsKeyEvictionInterval() time.Duration {
	return s.durationByName("jobs.key_eviction_interval")
}
func (s *OperationalStore) JobsKeyEvictionTTL() time.Duration {
	return s.durationByName("jobs.key_eviction_ttl")
}

func (s *OperationalStore) MetricsPollInterval() time.Duration {
	return s.durationByName("observability.metrics_poll_interval")
}
func (s *OperationalStore) TelemetryDashboardWindow() time.Duration {
	return s.durationByName("observability.telemetry_dashboard_window")
}
func (s *OperationalStore) TelemetryDetailBoostTTL() time.Duration {
	return s.durationByName("observability.telemetry_detail_boost_ttl")
}

func (s *OperationalStore) StorageBatchFlushInterval() time.Duration {
	return s.durationByName("storage.batch_flush_interval")
}
func (s *OperationalStore) StorageRollupInterval() time.Duration {
	return s.durationByName("storage.rollup_interval")
}

// Put validates and writes a batch of operational settings, then
// updates the in-memory snapshot. Bootstrap fields cause an error.
func (s *OperationalStore) Put(ctx context.Context, updates map[string]string, who string) error {
	if s.writer == nil {
		return fmt.Errorf("settings: store opened read-only")
	}

	allByName := map[string]FieldMeta{}
	for _, f := range AllFields() {
		allByName[f.Name] = f
	}

	for name, raw := range updates {
		f, ok := allByName[name]
		if !ok {
			return fmt.Errorf("settings: unknown setting %q", name)
		}
		if f.Class == ClassBootstrap {
			return fmt.Errorf("settings: %q is a bootstrap setting; edit config.toml or env", name)
		}
		if _, err := Validate(f, raw); err != nil {
			return err
		}
	}

	for name, raw := range updates {
		f := allByName[name]
		if strings.HasPrefix(f.Store, "panel_settings.") {
			col := strings.TrimPrefix(f.Store, "panel_settings.")
			if err := s.writer.WritePanelColumn(ctx, col, raw, who); err != nil {
				return err
			}
		} else if f.Store == "runtime_settings" {
			body, err := encodeRuntimeJSON(f.Type, raw)
			if err != nil {
				return err
			}
			if err := s.writer.WriteRuntimeSetting(ctx, f.Name, body, who); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("settings: %q has unknown store %q", name, f.Store)
		}
	}

	return s.Reload(ctx)
}

// SeedDefaults writes the first-boot seed (env > config.toml) into the
// store for every operational field that has a seed source and no value
// currently stored. Registry defaults are left implicit. Idempotent.
func (s *OperationalStore) SeedDefaults(ctx context.Context, in LoaderInput) error {
	if s.writer == nil {
		return fmt.Errorf("settings: SeedDefaults requires a writer")
	}
	env := envSliceToMap(in.Env)
	tomlVals, err := loadTOMLValues(in.ConfigPath)
	if err != nil {
		return fmt.Errorf("settings: SeedDefaults: %w", err)
	}
	updates := map[string]string{}
	for _, f := range AllFields() {
		if f.Class != ClassOperational {
			continue
		}
		if _, ok, _ := s.read(ctx, f); ok {
			continue
		}
		if v, _, seed := seedValue(f, env, tomlVals); seed {
			updates[f.Name] = v
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return s.Put(ctx, updates, "seed:firstboot")
}

func encodeRuntimeJSON(t Type, raw string) (string, error) {
	switch t {
	case TypeString, TypeEnum, TypeURL, TypeHostPort:
		b, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case TypeInt:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(n, 10), nil
	case TypeBool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case TypeDuration:
		j, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		return string(j), nil
	case TypeJSON:
		return raw, nil
	}
	return "", fmt.Errorf("settings: encodeRuntimeJSON: unsupported type %s", t)
}

// ActiveSnapshot is an immutable copy of operational values captured at
// process start. Used to detect pending restart-required changes.
type ActiveSnapshot struct{ values map[string]string }

// Get returns the active value for the named setting, if any.
func (a *ActiveSnapshot) Get(name string) (string, bool) {
	if a == nil {
		return "", false
	}
	v, ok := a.values[name]
	return v, ok
}

// CaptureActive returns a copy of the current snapshot. Call after the
// initial Reload to record the values that the running process
// "actually applied" — operational changes that require restart are
// detected by comparing live values against this baseline.
func (s *OperationalStore) CaptureActive() *ActiveSnapshot {
	snap := s.cache.Load()
	if snap == nil {
		return &ActiveSnapshot{values: map[string]string{}}
	}
	out := make(map[string]string, len(snap.values))
	for k, v := range snap.values {
		out[k] = v
	}
	return &ActiveSnapshot{values: out}
}

// PendingChanges returns the names of operational fields whose live
// value differs from `active` AND that are declared apply=restart.
func (s *OperationalStore) PendingChanges(active *ActiveSnapshot) []string {
	if active == nil {
		return nil
	}
	live := s.cache.Load()
	if live == nil {
		return nil
	}
	var out []string
	for _, f := range AllFields() {
		if f.Class != ClassOperational || f.Apply != ApplyRestart {
			continue
		}
		if live.values[f.Name] != active.values[f.Name] {
			out = append(out, f.Name)
		}
	}
	return out
}
