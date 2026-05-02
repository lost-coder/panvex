package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// R-Q-01/07: lifecycle (constructor + shutdown + seed) extracted
// from server.go so the Server type definition stays focused on
// shape; this file is the composition root.

// initSecrets resolves the time source, CSRF secret and secret vault. Any
// fatal failure (no entropy or unparseable encryption key) panics here so
// the operator notices at boot rather than later when sessions or certs
// silently break.
func initSecrets(options Options) (func() time.Time, []byte, *secretvault.Vault) {
	now := options.Now
	if now == nil {
		now = time.Now
	}

	csrfSecret, err := loadOrCreateCSRFSecret(options.Store)
	if err != nil {
		// crypto/rand.Read returning an error means the OS entropy pool
		// is unavailable — there is nothing meaningful the panel can do
		// without it (sessions, certs all need it too). Fail loudly so
		// an operator notices instead of falling back to CSRF-disabled
		// mode.
		panic("control-plane: cannot initialise CSRF secret: " + err.Error())
	}

	// Build the secret vault once from the operator passphrase. A nil or
	// empty passphrase yields a disabled vault so existing dev fixtures
	// keep using plaintext at-rest. The HKDF salt is per-install: load
	// from cp_secrets or mint+persist on first start so two deployments
	// sharing a master passphrase do not derive identical domain keys.
	saltBytes, saltErr := loadOrCreateVaultSalt(options.Store)
	if saltErr != nil {
		panic("control-plane: cannot resolve vault HKDF salt: " + saltErr.Error())
	}
	vault, vaultErr := secretvault.NewWithSalt(options.EncryptionKey, secretvault.AllDomains, saltBytes)
	if vaultErr != nil {
		panic("control-plane: cannot initialise secret vault: " + vaultErr.Error())
	}
	return now, csrfSecret, vault
}

// newServerFromOptions populates the Server struct literal. Pure data plumbing
// — no I/O, no error paths.
func newServerFromOptions(options Options, now func() time.Time, csrfSecret []byte, vault *secretvault.Vault) *Server {
	return &Server{
		auth:                         auth.NewService(),
		store:                        options.Store,
		uiFiles:                      options.UIFiles,
		jobs:                         jobs.NewService(),
		presence:                     presence.NewTracker(30*time.Second, 90*time.Second),
		events:                       eventbus.NewHub(),
		now:                          now,
		panelRuntime:                 defaultPanelRuntime(options.PanelRuntime),
		requestRestart:               options.RequestRestart,
		loginRateLimiter:             newFixedWindowRateLimiter(httpLoginRateLimitPerWindow, defaultRateLimitWindow),
		agentBootstrapRateLimiter:    newFixedWindowRateLimiter(httpAgentBootstrapRateLimitPerWindow, defaultRateLimitWindow),
		grpcConnectRateLimiter:       newFixedWindowRateLimiter(grpcConnectRateLimitPerWindow, defaultRateLimitWindow),
		sensitiveRateLimiter:         newFixedWindowRateLimiter(httpSensitiveRateLimitPerWindow, defaultRateLimitWindow),
		loginLockout:                 newAccountLockoutTracker(),
		trustedProxyCIDRs:            options.TrustedProxyCIDRs,
		encryptionKey:                options.EncryptionKey,
		secretVault:                  vault,
		logger:                       options.Logger,
		version:                      options.Version,
		commitSHA:                    options.CommitSHA,
		buildTime:                    options.BuildTime,
		csrfSecret:                   csrfSecret,
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
		lastUsageSeq:                 make(map[string]uint64),
		sessions:                     agents.NewSessionManager(),
		clientsSvc:                   clients.NewServiceWithVault(options.Store, now, vault),
		fleetSvc:                     fleet.NewService(options.Store, func() time.Time { return now().UTC() }),
		instances:                    make(map[string]Instance),
		metrics:                      make([]MetricSnapshot, 0, maxInMemoryMetricSnapshots),
		intervals:                    options.Intervals.withDefaults(),
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
	s.trySetStartupErr(s.restoreStoredState)
	s.trySetStartupErr(s.restoreStoredClients)
	s.trySetStartupErr(s.restoreStoredDiscoveredClients)
	s.trySetStartupErr(s.restoreStoredPanelSettings)
	// SetPasswordPolicy is called unconditionally: even if restoreStoredPanelSettings
	// failed and s.panelSettings is zero-valued, auth.effectivePolicy maps zero to
	// auth.DefaultPasswordMinLength (S-01). Do not move this call above the restore.
	s.auth.SetPasswordPolicy(s.panelSettings.PasswordMinLength)
	s.trySetStartupErr(s.restoreUpdateSettings)
	s.trySetStartupErr(s.restoreRetentionSettings)

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
	rollupCtx, rollupCancel := context.WithCancel(s.serverCtx)
	s.stopRollup = rollupCancel
	s.startTimeseriesRollupWorker(rollupCtx)
	s.startUpdateCheckerWorker(rollupCtx)

	// Evict idempotency keys for terminal jobs on an hourly tick to keep
	// jobs.Service.keys bounded. See P2-PERF-03. TTL of 24h matches the
	// operational expectation that clients will not retry the same
	// idempotency key after a full day.
	s.rollupWg.Add(1)
	s.jobs.StartKeyEvictionWorker(rollupCtx, s.intervals.JobsKeyEviction, s.intervals.JobsKeyEvictionTTL, &s.rollupWg)

	// P2-LOG-05 (L-14): expire acknowledged-but-never-resulted targets after
	// 2h so jobs do not stay "acknowledged" forever when the agent restarts
	// between ack and result. The 2h window matches the agent idempotency
	// cache so the CP gives up in sync with the agent's ability to safely
	// deduplicate.
	s.rollupWg.Add(1)
	s.jobs.StartAcknowledgedExpiryWorker(rollupCtx, s.intervals.JobsAckExpiry, s.intervals.JobsAckExpiryTTL, &s.rollupWg)

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
		s.startMetricsPoller(metricsCtx, s.intervals.MetricsPoller)
	}
}

func New(options Options) *Server {
	now, csrfSecret, vault := initSecrets(options)
	server := newServerFromOptions(options, now, csrfSecret, vault)
	// Lifecycle context — cancelled by Close(). Plan 3 Task 1 introduces
	// the field; Tasks 2-6 migrate the long-lived workers (rollup, metrics
	// poller, fleet-ensure, lockout-restore, batch-writer drain) onto it.
	server.serverCtx, server.serverCancel = context.WithCancel(context.Background())
	if server.logger == nil {
		server.logger = slog.Default()
	}
	// R-S-09: route lockout-tracker warnings through the same HMAC
	// redaction that http_auth.go uses so log aggregators never see raw
	// usernames even when the persistent store fails.
	server.loginLockout.SetRedactor(server.logUsername)
	server.panelSettings = defaultPanelSettings()
	server.updateSettings = defaultUpdateSettings()
	server.retention = defaultRetentionSettings()

	authority, err := loadOrCreateCertificateAuthority(options.Store, now(), options.EncryptionKey)
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
	warnIfTrustedProxyMisconfigured(server.logger, server.panelRuntime.HTTPListenAddress, server.trustedProxyCIDRs)

	server.startBackgroundWorkers()

	if server.store != nil {
		// Pass the Prometheus bundle as the metrics sink so batch writer
		// errors surface to operators (P2-REL-06 / H14). obs is always set
		// earlier in New(); nil would fall back to the no-op sink.
		server.batchWriter = newStoreBatchWriter(server.store, server.obs, server.now)
		server.batchWriter.Start()
	}

	return server
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
		if err := s.batchWriter.StopWithTimeout(10 * time.Second); err != nil {
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
