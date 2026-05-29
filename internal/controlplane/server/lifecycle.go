package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/csrf"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/fleet/integrations"
	"github.com/lost-coder/panvex/internal/controlplane/geoip"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/runtimeevents"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/sessions"
	"github.com/lost-coder/panvex/internal/controlplane/settings"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// R-Q-01/07: lifecycle (constructor + shutdown + seed) extracted
// from server.go so the Server type definition stays focused on
// shape; this file is the composition root.

// initSecrets resolves the time source, CSRF secret and secret vault.
// Any fatal failure (no entropy, unparseable encryption key, or storage
// error) is returned as a wrapped error so the constructor can surface
// it to the caller (Plan 3 Task 4 / Q-7). Library-level panics violate
// Go style — embedders and tests cannot recover — so the boot path
// bubbles errors instead of crashing the process from inside a
// dependency.
//
// bootCtx is the lifecycle context (Server.serverCtx) so a Close() while
// startup is still wedged on a slow GetCPSecret aborts the storage call
// instead of leaking past shutdown (Plan 3 Task 3).
func initSecrets(bootCtx context.Context, options Options) (func() time.Time, *csrf.Manager, *secretvault.Vault, error) {
	now := options.Now
	if now == nil {
		now = time.Now
	}

	csrfManager, err := csrf.NewManager(bootCtx, options.Store, slog.Default())
	if err != nil {
		// crypto/rand.Read returning an error means the OS entropy pool
		// is unavailable — there is nothing meaningful the panel can do
		// without it (sessions, certs all need it too). Fail loudly so
		// an operator notices instead of falling back to CSRF-disabled
		// mode.
		return nil, nil, nil, fmt.Errorf("init CSRF secret: %w", err)
	}

	// Build the secret vault once from the operator passphrase. A nil or
	// empty passphrase yields a disabled vault so existing dev fixtures
	// keep using plaintext at-rest. The HKDF salt is per-install: load
	// from cp_secrets or mint+persist on first start so two deployments
	// sharing a master passphrase do not derive identical domain keys.
	saltBytes, saltErr := loadOrCreateVaultSalt(bootCtx, options.Store)
	if saltErr != nil {
		return nil, nil, nil, fmt.Errorf("resolve vault HKDF salt: %w", saltErr)
	}
	// Wave 5.2: envelope encryption + key rotation. NewWithEnvelope
	// generates per-domain DEKs on first start (or loads + decrypts
	// the existing ones under the KEK on subsequent starts), and
	// records a KEK fingerprint so a typo'd PANVEX_ENCRYPTION_KEY
	// fails fast instead of silently producing garbage Decrypts.
	// Falls back to the bare per-install salt path when no passphrase
	// is set (dev installs) — NewWithEnvelope short-circuits to a
	// pass-through vault in that case.
	//
	// secretvault doesn't import the storage package (would cycle), so
	// we wrap options.Store in an adapter that translates the
	// storage.ErrNotFound sentinel to the empty-bytes convention the
	// vault's CPSecretReader interface uses for "no row".
	vault, vaultErr := secretvault.NewWithEnvelope(bootCtx, options.EncryptionKey, secretvault.AllDomains, saltBytes, vaultCPSecretAdapter{store: options.Store})
	if vaultErr != nil {
		return nil, nil, nil, fmt.Errorf("init secret vault: %w", vaultErr)
	}
	return now, csrfManager, vault, nil
}

// newServerFromOptions populates the Server struct literal. Pure data plumbing
// — no I/O, no error paths.
func newServerFromOptions(options Options, now func() time.Time, csrfManager *csrf.Manager, vault *secretvault.Vault) *Server {
	return &Server{
		auth:     auth.NewService(),
		store:    options.Store,
		uiFiles:  options.UIFiles,
		jobs:     jobs.NewService(),
		presence: presence.NewTracker(30*time.Second, 90*time.Second),
		events:   eventbus.NewHub(),
		// Runtime Events Phase 3: 500-event ring buffer per agent for
		// slog records shipped over the Connect bidi-stream. Always
		// constructed (independent of Store wiring) so the message
		// dispatcher and HTTP handler can rely on a non-nil pointer.
		runtimeEvents:                runtimeevents.New(500),
		now:                          now,
		panelRuntime:                 defaultPanelRuntime(options.PanelRuntime),
		requestRestart:               options.RequestRestart,
		loginRateLimiter:             newFixedWindowRateLimiter(httpLoginRateLimitPerWindow, defaultRateLimitWindow),
		agentBootstrapRateLimiter:    newFixedWindowRateLimiter(httpAgentBootstrapRateLimitPerWindow, defaultRateLimitWindow),
		grpcConnectRateLimiter:       newFixedWindowRateLimiter(grpcConnectRateLimitPerWindow, defaultRateLimitWindow),
		sensitiveRateLimiter:         newFixedWindowRateLimiter(httpSensitiveRateLimitPerWindow, defaultRateLimitWindow),
		loginLockout:                 newAccountLockoutTracker(),
		totpLockout:                  newTOTPLockoutTracker(),
		ipLockout:                    newIPLockoutTracker(),
		wsConnLimiter:                newWSConnLimiter(),
		trustedProxyCIDRs:            options.TrustedProxyCIDRs,
		encryptionKey:                options.EncryptionKey,
		secretVault:                  vault,
		logger:                       options.Logger,
		version:                      options.Version,
		commitSHA:                    options.CommitSHA,
		buildTime:                    options.BuildTime,
		csrfManager:                  csrfManager,
		loginTimingFloor:             resolveLoginTimingFloor(options.LoginTimingFloor),
		revokedAgentIDs:              make(map[string]struct{}),
		agents:                       make(map[string]Agent),
		detailBoosts:                 make(map[string]time.Time),
		initializationWatchCooldowns: make(map[string]time.Time),
		fallbackEnteredAt:            make(map[string]time.Time),
		clients:                      make(map[string]managedClient),
		clientAssignments:            make(map[string][]managedClientAssignment),
		clientDeployments:            make(map[string]map[string]managedClientDeployment),
		clientUsage:                  make(map[string]map[string]clientUsageSnapshot),
		agentClientUsage:             make(map[string]map[string]struct{}),
		lastUsageSeq:                 make(map[string]uint64),
		sessions:                     agents.NewSessionManager(),
		clientsSvc:                   clients.NewServiceWithVault(options.Store, now, vault),
		fleetSvc:                     fleet.NewService(options.Store, func() time.Time { return now().UTC() }),
		instances:                    make(map[string]Instance),
		metrics:                      make([]MetricSnapshot, 0, maxInMemoryMetricSnapshots),
		intervals:                    options.Intervals.withDefaults(),
		sqlitePath:                   options.SQLitePath,
		bootstrap:                    options.Bootstrap,
		bootstrapSources:             options.BootstrapSources,
	}
}

// initWebhookSubsystem builds the outbox storage + producer once
// the secret vault is available. Called from initStoreBackedSubsystems
// (which runs after initSecrets has produced the vault). When
// Options.WebhookStorageFactory is unset the subsystem stays
// disabled — webhookProducer remains nil and publishWebhookEvent is
// a no-op.
func (s *Server) initWebhookSubsystem(options Options, vault *secretvault.Vault) {
	if options.WebhookStorageFactory == nil {
		return
	}
	decrypt := func(ciphertext string) ([]byte, error) {
		// nil/zero vault is a documented pass-through (see
		// secretvault.Vault.Decrypt). Production deployments always
		// have a vault; dev/test installs without an encryption key
		// receive plaintext back unchanged.
		plain, err := vault.Decrypt(secretvault.DomainWebhookSecret, ciphertext)
		if err != nil {
			return nil, err
		}
		return []byte(plain), nil
	}
	s.webhookStorage = options.WebhookStorageFactory(decrypt)
	if s.webhookStorage != nil {
		s.webhookProducer = webhooks.NewProducer(s.webhookStorage)
	}
}

// trySetStartupErr runs fn only if no earlier startup step has already failed,
// recording its error into s.startupErr. Lets the constructor express its
// long startup pipeline as a flat sequence of `s.trySetStartupErr(...)` calls
// instead of nested `if startupErr == nil { if err := ...; err != nil { ... } }`.
func (s *Server) trySetStartupErr(fn func() error) {
	if s.startupErr != nil {
		return
	}
	if err := fn(); err != nil {
		s.startupErr = err
	}
}

// initStoreBackedSubsystems wires the auth/jobs/lockout services to their
// store backends, restores their persistent state, and seeds whatever the
// fresh database needs (default fleet group, seeded users). Errors are
// short-circuited via trySetStartupErr.
func (s *Server) initStoreBackedSubsystems(options Options, vault *secretvault.Vault) {
	store := options.Store
	s.jobs = jobs.NewServiceWithStore(store)
	s.auth = auth.NewServiceWithStore(store)
	s.auth.SetSessionStore(store)
	s.auth.SetVault(vault)
	s.auth.SetConsumedTotpStore(store)
	// Seal integration-provider credentials at rest under the same vault.
	if s.fleetSvc != nil {
		s.fleetSvc.SetVault(vault)
		// Register the concrete provider kinds. Registration is fatal on
		// failure (duplicate / nil kind) — a misconfigured registry would
		// otherwise silently disable provider validation and, worse, leave
		// secret-field redaction fail-safing the whole config blob.
		s.trySetStartupErr(func() error {
			return s.fleetSvc.ProviderRegistry().Register(integrations.NewCloudflareProvider())
		})
	}
	// Webhook outbox subsystem: builds Storage + Producer if the
	// caller wired a factory. Must run after the vault is available
	// (it provides the SecretDecrypter), and before
	// startBackgroundWorkers (which spawns the worker goroutine).
	s.initWebhookSubsystem(options, vault)
	// S22 Task 5 (S-medium): derive the per-server session-lookup HMAC
	// key from EncryptionKey under a unique domain tag. The DB primary
	// key for a session row is HMAC(opaque cookie token) under this
	// key, so a leaked DB row plus the audit log-redaction key cannot
	// be correlated back to a live cookie. Empty EncryptionKey leaves
	// the lookup key unset; the auth service falls back to a fresh
	// per-process random key on first use (cookies issued before the
	// process restarts under that fallback are then unrecoverable —
	// production must always set EncryptionKey for cross-restart
	// continuity). Any error from SetSessionLookupKey here is fatal
	// because the alternative is silently shipping with a misconfigured
	// or weak lookup key.
	s.trySetStartupErr(func() error {
		key := deriveSessionLookupKey(s.encryptionKey)
		if key == nil {
			return nil
		}
		return s.auth.SetSessionLookupKey(key)
	})
	s.trySetStartupErr(s.auth.RestoreSessions)

	// S7: wire the lockout tracker to the persistent backend and load any
	// state that survived a restart. We attach the store before the restore
	// so subsequent writes are persisted even if the initial restore was
	// empty.
	s.loginLockout.SetStore(newLockoutStoreAdapter(store))
	s.trySetStartupErr(func() error {
		return s.loginLockout.Restore(s.serverCtx, s.now())
	})
	s.trySetStartupErr(s.jobs.StartupError)
	s.trySetStartupErr(func() error { return s.seedUsers(options.Users) })
	s.trySetStartupErr(func() error {
		return s.restoreStoredState(s.serverCtx)
	})
	// Phase 7: wire clientsSvc with NewServiceV2 when a raw *sql.DB is
	// available. Both SQLite and Postgres stores expose DB() *sql.DB; the
	// concrete type determines which backend constructors to use.
	// When the store does not expose DB() (e.g. test doubles), clientsSvc
	// keeps its NewServiceWithVault wiring from newServerFromOptions.
	s.trySetStartupErr(func() error {
		rawDBer, ok := store.(interface{ DB() *sql.DB })
		if !ok {
			return nil
		}
		rawDB := rawDBer.DB()
		if rawDB == nil {
			return nil
		}
		var (
			clientsRepo      clients.Repository
			discoveredRepoV2 discovered.Repository
			uowImpl          uow.UnitOfWork
		)
		switch store.(type) {
		case *sqlite.Store:
			clientsRepo = sqlite.NewClientsRepository(rawDB)
			discoveredRepoV2 = sqlite.NewDiscoveredRepository(rawDB)
			uowImpl = sqlite.NewUoW(rawDB)
		case *postgres.Store:
			clientsRepo = postgres.NewClientsRepository(rawDB)
			discoveredRepoV2 = postgres.NewDiscoveredRepository(rawDB)
			uowImpl = postgres.NewUoW(rawDB)
		default:
			if options.ClientsRepoOverride == nil {
				// Unknown concrete store type with no override — fall back to
				// NewServiceWithVault wiring already set in newServerFromOptions.
				return nil
			}
			// Test double wrapping a real SQLite store: use the override repo
			// for clients while building UoW and discoveredRepo from rawDB
			// (SQLite assumed; tests always use SQLite via failingStore).
			clientsRepo = options.ClientsRepoOverride
			discoveredRepoV2 = sqlite.NewDiscoveredRepository(rawDB)
			uowImpl = sqlite.NewUoW(rawDB)
		}
		uowAdapter := newClientsUoWAdapter(uowImpl)
		if options.ClientsRepoOverride != nil {
			// Failure-injection tests need rs.Clients() inside SaveState's UoW
			// callback to return the override repo (not the tx-bound real one).
			uowAdapter = newClientsUoWAdapterWithOverride(uowImpl, options.ClientsRepoOverride)
		}
		s.clientsSvc = clients.NewServiceV2(clients.ServiceConfig{
			Repo:           clientsRepo,
			DiscoveredRepo: discoveredRepoV2,
			UoW:            uowAdapter,
			Vault:          vault,
			Now:            s.now,
			Store:          store, // legacy methods coexist during phase 7
		})
		s.discoveredRepo = discoveredRepoV2
		s.uow = uowImpl
		return nil
	})
	s.trySetStartupErr(s.restoreStoredClients)
	s.trySetStartupErr(s.restoreStoredDiscoveredClients)
	s.trySetStartupErr(s.restoreStoredPanelSettings)
	// SetPasswordPolicy is called unconditionally: even if restoreStoredPanelSettings
	// failed and s.panelSettings is zero-valued, auth.effectivePolicy maps zero to
	// auth.DefaultPasswordMinLength (S-01). Do not move this call above the restore.
	s.auth.SetPasswordPolicy(s.panelSettings.PasswordMinLength)
	s.trySetStartupErr(s.restoreUpdateSettings)
	s.trySetStartupErr(s.restoreRetentionSettings)
	// Restore persisted geoip settings + state and reload the manager
	// from disk if the configured .mmdb paths exist. Failures are
	// fatal at boot so a corrupt settings blob does not silently
	// disable lookups.
	s.trySetStartupErr(s.restoreGeoIPSettings)

	// Wire the operational settings store. Type-assert the concrete store to
	// obtain *sql.DB without growing the storage.Store interface — only the
	// sqlite and postgres concrete types need to expose DB(), and lifecycle
	// code is the only caller.
	if rawDBer, ok := options.Store.(interface{ DB() *sql.DB }); ok {
		if rawDB := rawDBer.DB(); rawDB != nil {
			// SQLitePath is empty for postgres deployments (set only for sqlite).
			ph := settings.PlaceholderQ
			if options.SQLitePath == "" {
				ph = settings.PlaceholderDollar
			}
			s.settings = settings.NewOperationalStoreRW(
				settings.NewDBStore(rawDB, ph),
				settings.NewDBStore(rawDB, ph),
			)
			// Plan 6: the listen addresses are now operational settings
			// seeded from env/config on first boot. Enable read-time env
			// overrides (PANVEX_HTTP_ADDR/PANVEX_GRPC_ADDR still win) and
			// persist any env/toml seed into the store so the UI shows
			// source=db and the value survives env removal. This MUST run
			// before Reload/CaptureActive so the captured baseline and the
			// listener bind both observe the seeded values.
			//
			// Seeding is best-effort: a failure to persist the seed (e.g. a
			// missing/unreadable config.toml) must NOT prevent the
			// subsequent Reload, which is the step that populates the
			// effective values (env-override > stored > registry default).
			// RawByName is correct without seeding; seeding only adds DB
			// persistence + source=db in the UI. Gating Reload behind the
			// seed would otherwise boot the panel with an empty settings
			// cache — the exact startup-reorder hazard this plan guards.
			s.settings.UseEnv(os.Environ())
			if err := s.settings.SeedDefaults(s.serverCtx, settings.LoaderInput{
				ConfigPath: s.panelRuntime.ConfigPath,
				Env:        os.Environ(),
			}); err != nil {
				s.logger.Error("settings seed to DB failed; env/config values are live this boot but will NOT persist (they revert to defaults once the env var is removed) — investigate the settings store",
					"error", err, "alert", "settings_seed_persist_failed")
			}
			s.trySetStartupErr(func() error {
				return s.settings.Reload(s.serverCtx)
			})
			s.settingsActive = s.settings.CaptureActive()
			// Mirror the password policy from the now-reloaded store. The
			// line ~308 SetPasswordPolicy call ran before the store existed
			// (no-store fallback); with the store wired, new writes go to
			// scope=default not the legacy s.panelSettings, so re-apply from
			// the store here or the policy stays stale across restarts (S-01).
			if s.settings != nil {
				s.auth.SetPasswordPolicy(int32(s.settings.PasswordMinLength())) //nolint:gosec // bounded 8-64 in registry
			}
			// Capture apply=restart session fields once at startup. A change
			// to these fields only takes effect after an operator-initiated
			// panel restart; the running process uses the values below.
			s.activeSessionIdleTimeout = s.settings.AuthSessionIdleTimeout()
			s.activeSessionMaxLifetime = s.settings.AuthSessionMaxLifetime()

			// Enrollment-logging Phase 1 / Task 13: wire the timeline
			// recorder so the inbound bootstrap handler (and later the
			// outbound flow) can record per-attempt steps and surface
			// them on the /events bus the dashboard already subscribes
			// to. Construction is gated on DB() — test fixtures with
			// mock stores leave enrollmentRec nil, and handlers must
			// nil-check before calling.
			s.enrollmentRec = enrollment.NewRecorder(
				enrollment.NewSQLStore(dbsqlc.New(rawDB), rawDB),
				s.now,
			).
				WithPublisher(enrollmentBusAdapter{bus: s.events}).
				WithLogger(s.logger)
		}
	}

	// Fresh databases need at least one fleet group so enrollment tokens can
	// reference it. Operators can rename the label afterwards via the HTTP
	// API; the `default` slug is kept so docs and scripts can rely on a
	// predictable name.
	s.trySetStartupErr(func() error {
		_, err := s.fleetSvc.EnsureDefault(s.serverCtx)
		return err
	})
}

// startBackgroundWorkers launches the rollup, key-eviction, ack-expiry and
// metrics-poller goroutines. Returns the rollup ctx so the caller stays in
// charge of cleanup wiring.
func (s *Server) startBackgroundWorkers() {
	// Resolve worker cadences: prefer OperationalStore getters when a store
	// is wired (production); fall back to s.intervals for tests and
	// no-persistent-store configurations.
	rollupInterval := s.intervals.Rollup
	keyEvictionInterval := s.intervals.JobsKeyEviction
	keyEvictionTTL := s.intervals.JobsKeyEvictionTTL
	ackExpiryInterval := s.intervals.JobsAckExpiry
	ackExpiryTTL := s.intervals.JobsAckExpiryTTL
	metricsPoller := s.intervals.MetricsPoller
	if s.settings != nil {
		rollupInterval = s.settings.StorageRollupInterval()
		keyEvictionInterval = s.settings.JobsKeyEvictionInterval()
		keyEvictionTTL = s.settings.JobsKeyEvictionTTL()
		ackExpiryInterval = s.settings.JobsAckExpiryInterval()
		ackExpiryTTL = s.settings.JobsAckExpiryTTL()
		metricsPoller = s.settings.MetricsPollInterval()
	}

	//nolint:gosec // G118: rollupCancel is assigned to s.stopRollup on the next line and invoked from Close().
	rollupCtx, rollupCancel := context.WithCancel(s.serverCtx)
	s.stopRollup = rollupCancel
	s.startTimeseriesRollupWorker(rollupCtx, rollupInterval)
	s.startUpdateCheckerWorker(rollupCtx)

	// Evict idempotency keys for terminal jobs on an hourly tick to keep
	// jobs.Service.keys bounded. See P2-PERF-03. TTL of 24h matches the
	// operational expectation that clients will not retry the same
	// idempotency key after a full day.
	s.rollupWg.Add(1)
	s.jobs.StartKeyEvictionWorker(rollupCtx, keyEvictionInterval, keyEvictionTTL, &s.rollupWg)

	// P2-LOG-05 (L-14): expire acknowledged-but-never-resulted targets after
	// 2h so jobs do not stay "acknowledged" forever when the agent restarts
	// between ack and result. The 2h window matches the agent idempotency
	// cache so the CP gives up in sync with the agent's ability to safely
	// deduplicate.
	s.rollupWg.Add(1)
	s.jobs.StartAcknowledgedExpiryWorker(rollupCtx, ackExpiryInterval, ackExpiryTTL, &s.rollupWg)

	// Spawn the geoip auto/URL refresh worker. No-op when mode is
	// disabled or local — those modes do not pull files from the
	// network. The worker registers itself with rollupWg so Close()
	// joins on it before returning.
	s.startGeoIPUpdaterWorker(rollupCtx)

	// The metrics poller samples derived gauges (agent connected count,
	// event-hub subscribers, job queue depth, lockout count) on a 5-second
	// interval. Runs in its own context so Close() can stop it independently
	// of the rollup workers.
	//
	// Gate on MetricsScrapeToken: if no token is configured, the /metrics
	// endpoint is not registered, so nobody can observe these gauges.
	// Skipping the poller when metrics are disabled keeps the race-free
	// clock-mock pattern working for tests.
	if s.metricsScrapeToken != "" {
		metricsCtx, metricsCancel := context.WithCancel(s.serverCtx)
		s.metricsPollerCancel = metricsCancel
		s.startMetricsPoller(metricsCtx, metricsPoller)
	}

	// Webhook outbox worker — no-op when WebhookStorage was not
	// supplied (Producer is also nil; event sources skip publish).
	// Lives on rollupCtx so Close()'s rollupCancel reaps it together
	// with the other long-lived background loops.
	s.startWebhookWorker(rollupCtx, s.webhookStorage)

	// Enrollment attempts retention worker — prunes attempt rows older
	// than the configured retention so the timeline table does not grow
	// unbounded. Defaults to 30 days; PANVEX_ENROLLMENT_RETENTION_DAYS
	// overrides at boot, and setting it to 0 disables retention
	// entirely. Lives on rollupCtx for the same reason as the other
	// long-lived loops above.
	s.startEnrollmentCleanupWorker(rollupCtx, enrollmentRetention())
}

// enrollmentRetention resolves the attempt retention window from the
// environment, defaulting to 30 days. Returning a time.Duration (rather
// than days as int) keeps the boot-time conversion in one place; the
// worker treats ≤0 as "disabled".
func enrollmentRetention() time.Duration {
	const defaultDays = 30
	days := defaultDays
	if v := os.Getenv("PANVEX_ENROLLMENT_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			days = n
		}
	}
	return time.Duration(days) * 24 * time.Hour
}

// New constructs the control-plane Server. Returns an error (Plan 3
// Task 4 / Q-7) when boot-time secret initialisation fails — CSRF
// secret load, vault HKDF salt resolution, secret-vault construction,
// or username log-hash key derivation. The lifecycle context created
// up front is cancelled before returning the error so a wedged
// storage call started during initSecrets does not outlive the failed
// constructor.
func New(options Options) (*Server, error) {
	// Lifecycle context — cancelled by Close(). Plan 3 Task 1 introduces
	// the field; Tasks 2-6 migrate the long-lived workers (rollup, metrics
	// poller, fleet-ensure, lockout-restore, batch-writer drain) onto it.
	// Created BEFORE initSecrets so a slow CSRF/HKDF/CA storage call at
	// boot can be aborted by Close() (Plan 3 Task 3).
	bootCtx, bootCancel := context.WithCancel(context.Background())
	now, csrfManager, vault, err := initSecrets(bootCtx, options)
	if err != nil {
		bootCancel()
		return nil, err
	}
	server := newServerFromOptions(options, now, csrfManager, vault)
	server.serverCtx, server.serverCancel = bootCtx, bootCancel
	if server.logger == nil {
		server.logger = slog.Default()
	}
	// Build the geoip Manager up front (logger only) so handlers can
	// safely call into it before restoreGeoIPSettings runs. The Manager
	// owns no files until Reload — which is invoked later from
	// restoreGeoIPSettings if the configured paths exist on disk.
	server.geoip = geoip.NewManager(server.logger)
	// R-S-09: route lockout-tracker warnings through the same HMAC
	// redaction that http_auth.go uses so log aggregators never see raw
	// usernames even when the persistent store fails. The HMAC key is
	// primed up front (Plan 3 Task 4 / Q-7) so the redactor never has
	// to derive entropy on the hot path and entropy failures surface as
	// a New() error rather than a library-level panic.
	if err := server.initUsernameHashKey(); err != nil {
		bootCancel()
		return nil, err
	}
	server.loginLockout.SetRedactor(server.logUsername)
	server.panelSettings = defaultPanelSettings()
	server.updateSettings = defaultUpdateSettings()
	server.retention = defaultRetentionSettings()

	authority, err := loadOrCreateCertificateAuthority(server.serverCtx, options.Store, now(), options.EncryptionKey)
	if err != nil {
		server.startupErr = err
	} else {
		server.authority = authority
	}

	switch {
	case options.Store != nil:
		server.initStoreBackedSubsystems(options, vault)
	case len(options.Users) > 0:
		server.auth.LoadUsers(options.Users)
	}

	// Rebuild the presence tracker with thresholds from the operational
	// settings store (Task 5). Must run after initStoreBackedSubsystems so
	// s.settings has been reloaded. When s.settings is nil (no persistent
	// store, e.g. tests), the tracker keeps the defaults wired above.
	if server.settings != nil {
		server.presence = presence.NewTracker(
			server.settings.AgentsPresenceDegradedAfter(),
			server.settings.AgentsPresenceOfflineAfter(),
		)
	}

	// Wire settings-backed thresholds into auth / lockout subsystems
	// (Task 6). Must run after initStoreBackedSubsystems so s.settings
	// and s.activeSession* are populated. All setters are no-ops when
	// their argument is nil — tests that construct without a store keep
	// the compiled-in constant behaviour unchanged.
	if server.settings != nil {
		// restart=false: re-read on each evaluation via live getters.
		server.loginLockout.SetThresholds(
			server.settings.AuthPasswordLockoutMaxAttempts,
			server.settings.AuthPasswordLockoutDuration,
		)
		// TOTP lockout: duration is audited (restart=false); max attempts is
		// not an audited tunable — keep the compiled-in constant.
		server.totpLockout.SetThresholds(
			sessions.TOTPLockoutMaxAttempts,
			server.settings.AuthTOTPLockoutDuration,
		)
		// TOTP setup TTL: live getter, restart=false.
		server.auth.SetTOTPSetupTTLFn(server.settings.AuthTOTPSetupTTL)
		// apply=restart: captured once at startup — return the fixed value.
		idleTimeout := server.activeSessionIdleTimeout
		maxLifetime := server.activeSessionMaxLifetime
		server.auth.SetSessionTimeoutFns(
			func() time.Duration { return idleTimeout },
			func() time.Duration { return maxLifetime },
		)
	}

	// Metrics collectors are always constructed (observation is cheap) but
	// the /metrics HTTP route is only registered when a scrape token is set.
	// This keeps the in-process counters available for internal consumption
	// (e.g. tests, future admin-only endpoints) without exposing them.
	server.obs = newMetricsCollectors()
	server.metricsScrapeToken = options.MetricsScrapeToken
	server.events.SetDropHook(func() {
		server.obs.eventHubDropTotal.Inc()
	})
	server.handler = server.routes()
	server.auth.SetNow(now)
	server.jobs.SetNow(now)

	// S-06: warn once at startup if the panel binds to a non-loopback
	// address but no trusted-proxy CIDRs are configured. In that state
	// X-Forwarded-For is silently ignored and rate-limit buckets the
	// entire fleet as one client.
	// Plan 6: the bind address now lives in the settings store; read it via
	// the effective accessor so the non-loopback check reflects the address
	// the listener will actually use (env-override > db-seed > default).
	warnIfTrustedProxyMisconfigured(server.logger, server.EffectiveHTTPListenAddress(), server.trustedProxyCIDRs)

	server.startBackgroundWorkers()

	if server.store != nil {
		// Pass the Prometheus bundle as the metrics sink so batch writer
		// errors surface to operators (P2-REL-06 / H14). obs is always set
		// earlier in New(); nil would fall back to the no-op sink.
		server.batchWriter = newStoreBatchWriter(server.store, server.obs, server.now)
		if server.settings != nil {
			server.batchWriter.flushInterval = server.settings.StorageBatchFlushInterval()
		}
		server.batchWriter.Start(server.serverCtx)
	}

	return server, nil
}

// Close stops background workers and drains pending writes. It should be
// called before closing the storage backend.
//
// Shutdown ordering (P2-LOG-10 / M-R4 / P7-R6):
//  1. batchWriter.StopWithTimeout(10s) FIRST — drains the audit-events
//     queue (and the 7 other streams) before any background goroutine that
//     might still be producing audits is shut down. This bounds the
//     worst-case shutdown time at 10s so a wedged DB cannot hang the
//     process indefinitely, while still giving the DB a realistic window
//     to absorb in-flight rows.
//  2. metrics / rollup goroutines stop afterwards.
//
// Events enqueued AFTER this point may race with the final drain and can
// be dropped — upstream callers (HTTP handlers, gRPC streams) must stop
// before Close() runs to guarantee zero loss.
func (s *Server) Close() {
	// Cancel the lifecycle context FIRST so any worker subscribed to it
	// observes shutdown before the batch writer drain or rollup stop runs.
	// Tasks 2-6 will migrate workers onto serverCtx; until then this is
	// effectively a no-op for the existing workers (which still use their
	// own cancel funcs) but the contract — Close cancels Context() — must
	// hold from this task forward. Idempotent under concurrent invocation:
	// sync.Once guarantees cancel runs exactly once even if two goroutines
	// race into Close() simultaneously (a bare nil-check + assign would
	// race; see the regression test in lifecycle_test.go).
	s.serverCloseOnce.Do(func() {
		if s.serverCancel != nil {
			s.serverCancel()
		}
	})
	if s.batchWriter != nil {
		// serverCtx was cancelled above so we detach from cancellation
		// (values still propagate) — otherwise the WithTimeout below would
		// be born already-cancelled and the drain would abort before any
		// queued audit row could be flushed. Plan 3 / BP-01.
		drainParent := context.WithoutCancel(s.serverCtx)
		if err := s.batchWriter.StopWithTimeout(drainParent, 10*time.Second); err != nil {
			s.logger.Error("batch writer drain timed out on shutdown",
				"error", err,
				"alert", "audit_persist_failed",
			)
		}
	}
	s.metricsShutdown()
	if s.stopRollup != nil {
		s.stopRollup()
	}
	// Wait for the rollup goroutine to finish before closing the store,
	// so it does not query a closed storage backend.
	s.rollupWg.Wait()
	// Close the geoip Manager AFTER the worker has joined via rollupWg
	// so we never close a reader that is mid-Reload. Idempotent.
	if s.geoip != nil {
		_ = s.geoip.Close()
	}
	// N-1: wait for any operator-driven background goroutines (panel
	// self-update, manual update-check) so a graceful restart cannot
	// race a half-applied binary swap.
	s.bgWG.Wait()
}

func (s *Server) seedUsers(users []auth.User) error {
	if s.store == nil || len(users) == 0 {
		return nil
	}

	records, err := s.store.ListUsers(s.serverCtx)
	if err != nil {
		return err
	}
	if len(records) > 0 {
		return nil
	}

	for _, user := range users {
		if err := s.store.PutUser(s.serverCtx, storage.UserRecord{
			ID:           user.ID,
			Username:     user.Username,
			PasswordHash: user.PasswordHash,
			Role:         string(user.Role),
			TotpEnabled:  user.TotpEnabled,
			TotpSecret:   user.TotpSecret,
			CreatedAt:    user.CreatedAt.UTC(),
		}); err != nil {
			return err
		}
	}

	return nil
}
