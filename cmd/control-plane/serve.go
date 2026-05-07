package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/bootstrap"
	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/server"
	"github.com/lost-coder/panvex/internal/controlplane/settings"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type serveConfig struct {
	ConfigPath           string
	ConfigManagedRuntime bool
	HTTPAddr             string
	HTTPRootPath         string
	AgentHTTPRootPath    string
	PanelAllowedCIDRs    []string
	GRPCAddr             string
	RestartMode          string
	TLSMode              string
	TLSCertFile          string
	TLSKeyFile           string
	Storage              config.StorageConfig
	TrustedProxyCIDRs    []*net.IPNet
	EncryptionKey        string
	LogLevel             string
	// LogFile, when non-empty, mirrors every log line into the named
	// path in addition to stderr. The file is opened in append mode.
	// Rotation is delegated to the host (logrotate, systemd-journal, …)
	// — the panel does not rotate it itself.
	LogFile          string
	Boot             *settings.Bootstrap
	BootstrapSources settings.SourceMap
}

func runServe(args []string) error {
	options, err := parseServeConfig(args)
	if err != nil {
		return err
	}

	// Wrap the text handler with the per-request slog handler so every
	// log line emitted from inside an HTTP handler picks up request_id
	// from the request context (see server.requestIDMiddleware).
	logSink, logCloser, err := openLogSink(options.LogFile)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	if logCloser != nil {
		defer func() { _ = logCloser() }()
	}
	baseHandler := slog.NewTextHandler(logSink, &slog.HandlerOptions{Level: parseLogLevel(options.LogLevel)})
	logger := slog.New(server.NewSlogContextHandler(baseHandler))
	slog.SetDefault(logger)
	if options.LogFile != "" {
		logger.Info("logging to file", "path", options.LogFile)
	}

	otelShutdown := initOtelTracing()
	// Shutdown BEFORE store.Close/api.Close so exporter can flush while
	// the process is still otherwise healthy. defer LIFO => this runs
	// first among the defers registered below it.
	defer shutdownOtel(otelShutdown)

	store, err := openStore(options.Storage)
	if err != nil {
		return err
	}
	defer store.Close() // closed after api.Close() stops background workers

	panelRuntime, err := resolvePanelRuntime(options)
	if err != nil {
		return err
	}
	restartRequests := make(chan struct{}, 1)

	api, err := newAPIServer(options, store, logger, panelRuntime, restartRequests)
	if err != nil {
		// Plan 3 Task 4 (Q-7): server.New now returns boot-time
		// initialisation failures (CSRF/HKDF/vault/log-hash key)
		// instead of panicking. Surface them as a normal CLI error
		// path so the operator gets a clean message and a non-zero
		// exit instead of a stack trace.
		return fmt.Errorf("control-plane init: %w", err)
	}
	// S-07: opt-in separate-listener pprof. PANVEX_PPROF_ADDR (e.g.
	// 127.0.0.1:6060) tells the panel to skip the admin-router /debug/pprof
	// registration and bring up a dedicated loopback listener instead.
	// Operator reaches it via `ssh -L 6060:localhost:6060 host`. Empty env
	// preserves the existing admin-router behavior.
	api.SetPprofListenerAddr(strings.TrimSpace(os.Getenv("PANVEX_PPROF_ADDR")))
	// Shutdown order (enforced by defer stack, LIFO):
	//   1. HTTP server Shutdown — stops accepting new panel/API requests so
	//      no new audit or metric events are produced.
	//   2. gRPC GracefulStop — drains active agent streams so upstream
	//      producers stop before api.Close runs the final batch drain.
	//   3. api.Close — final batch_writer drain + in-memory state flush.
	//   4. store.Close — close the underlying SQL/pgx pool.
	//
	// P3-REL-01: api.Close and store.Close are deferred here (not only in the
	// run loop) so that a panic, early return, or unexpected exit still
	// flushes buffered audit events before the process exits.
	defer api.Close()
	if err := api.StartupError(); err != nil {
		return err
	}

	// S-07: bring up the dedicated pprof listener if PANVEX_PPROF_ADDR was set.
	// Failing to bind means the admin opted in but cannot use the feature —
	// fail-closed rather than silently re-registering on the admin router.
	pprofShutdown, err := startPprofListenerIfConfigured(api)
	if err != nil {
		return err
	}
	if pprofShutdown != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = pprofShutdown(ctx)
		}()
	}

	// P3-OBS-01: wrap the chi-rooted handler with otelhttp so every
	// inbound HTTP request becomes a root span. When tracing is
	// disabled (no endpoint set) the global no-op TracerProvider makes
	// this effectively free.
	httpHandler := otelhttp.NewHandler(api.Handler(), "panvex-api")
	httpServer := newControlPlaneHTTPServer(panelRuntime.HTTPListenAddress, httpHandler)

	// Use ListenConfig with a Background ctx — the listener is meant to
	// outlive any single request and is closed via grpcListener.Close()
	// in the shutdown sequence above. ctx-aware Listen lets the kernel
	// surface errors via the cancellation channel; Background is correct
	// here because the listener has no notion of "request lifetime".
	listenConfig := net.ListenConfig{}
	grpcListener, err := listenConfig.Listen(context.Background(), "tcp", panelRuntime.GRPCListenAddress)
	if err != nil {
		return err
	}

	grpcServer := newControlPlaneGRPCServer(api.GRPCTLSConfig())
	gatewayrpc.RegisterAgentGatewayServer(grpcServer, api)

	// Шов 1 & 2: obtain *dbsqlc.Queries from the concrete store so the
	// agenttransport.Manager, bootstrap.InstallCommandHandler, and
	// bootstrap.EnrollDriver can execute transport/bootstrap SQL directly.
	// Both sqlite.Store and postgres.Store expose a Queries() method that
	// wraps the same connection pool. If the runtime store doesn't implement
	// the interface (e.g. a test double) we fall back to nil and the
	// downstream nil-guards keep the server functional without these features.
	type queriesProvider interface {
		Queries() *dbsqlc.Queries
	}
	var queries *dbsqlc.Queries
	if qp, ok := store.(queriesProvider); ok {
		queries = qp.Queries()
	} else {
		logger.Warn("storage backend does not expose Queries(); install-command and enrollment pre-flight disabled")
	}

	// agenttransport.Manager owns outbound supervisors and (in a later task)
	// the inbound dispatch path. Now wired with real DB queries so outbound
	// supervisors are restored at startup and the enrollment pre-flight runs.
	// The outbound TLS config is the panel's server-side mTLS config (agents
	// must present a cert signed by the panel CA). The CA is available via
	// api.GRPCTLSConfig() once the server is constructed above.
	outboundTLS := api.GRPCTLSConfig()
	manager := agenttransport.NewManager(queries, api.RunAgentSession, outboundTLS, logger)
	api.SetAgentTransportManager(manager)

	// Шов 1: wire the install-command handler so POST /agents/{id}/install-command
	// returns a curl | bash one-liner instead of 503. PanelURL is the gRPC
	// endpoint agents dial. ScriptURL, PanelCAPin, and PanelCN are derived
	// from the panel's CA certificate (same CA that signs agent certs).
	//
	// Q-05: ScriptURL is the panel's own /install-agent.sh route — see
	// internal/controlplane/server/install_script.go. The script is embedded
	// into the binary, so the curl|bash one-liner the install-command handler
	// generates works against any reachable panel host. Operators with a
	// custom domain set PANVEX_INSTALL_SCRIPT_URL to override.
	if queries != nil {
		installHandler := bootstrap.NewInstallCommandHandler(queries, bootstrap.InstallCommandConfig{
			ScriptURL: installScriptURL(panelRuntime),
			// S-3: bind the install-command to the embedded script body
			// the panel is serving right now. The shell one-liner verifies
			// the downloaded body before sudo-bash, and the script self-
			// checks PANVEX_INSTALL_SCRIPT_SHA256 (T-5). A TLS-MITM that
			// rewrites /install-agent.sh therefore cannot escalate.
			ScriptHash: server.InstallScriptSHA256(),
			PanelCAPin: api.CAPINHex(),
			PanelCN:    api.CACN(),
			PanelURL:   panelRuntime.GRPCListenAddress,
			Now:        time.Now,
		})
		api.SetInstallCommandHandler(installHandler)
	}

	// Шов 2: wire the enrollment pre-flight into the outbound supervisor pool.
	// EnrollDriver.Run is called before the mTLS dial when bootstrap_state=pending.
	//
	// S-2: the bootstrap TLS dialer used to set bare InsecureSkipVerify, which
	// left a window between the TLS handshake and the in-band token check
	// where a MITM could intercept the CSR exchange and proxy the bootstrap
	// token. We now substitute a VerifyPeerCertificate callback that pins
	// the agent's SPKI on a Trust-On-First-Use basis, keyed by agent ID
	// (see newBootstrapTLSConfig). InsecureSkipVerify is still set because
	// the agent's enrollment cert is self-signed (no chain to verify), but
	// any subsequent reconnect against a different leaf cert is rejected.
	if queries != nil {
		enrollDriver := bootstrap.NewEnrollDriver(queries, api.CertificateAuthority(), logger, time.Now)
		enrollDriver.SetCertPinWriter(store) // persist SPKI pin on each successful enroll (S-02)
		api.WireEnrollDriver(enrollDriver)

		enrollFn := func(ctx context.Context, agentAddr, agentID string) error {
			// Build the TLS config per-call so the VerifyPeerCertificate
			// callback can close over agentID and ctx for the storage
			// pin lookup. store satisfies bootstrapPinReader via its
			// GetAgentCertPin method.
			bootstrapTLS := newBootstrapTLSConfig(ctx, agentID, store)
			return enrollDriver.Run(ctx, agentAddr, bootstrapTLS, agentID)
		}
		bootstrapStateFn := func(ctx context.Context, agentID string) (string, error) {
			row, err := queries.GetAgentTransport(ctx, agentID)
			if err != nil {
				return "", err
			}
			return row.BootstrapState, nil
		}
		manager.SetEnrollCallbacks(enrollFn, bootstrapStateFn)
	}

	if err := manager.Start(context.Background()); err != nil {
		return fmt.Errorf("start agent transport manager: %w", err)
	}
	// Shutdown order: shutdownServers (HTTP + gRPC drain) runs before any
	// defer; manager.Stop tears down outbound supervisors AFTER gRPC has
	// drained but BEFORE api.Close flushes batch writers.
	defer manager.Stop()

	shutdownServers := func() {
		shutdownHTTPAndGRPC(httpServer, grpcServer, grpcListener)
	}

	httpErrors := make(chan error, 4)
	startHTTPServer(httpServer, panelRuntime, httpErrors)
	startGRPCServer(grpcServer, grpcListener, panelRuntime.GRPCListenAddress, httpErrors)

	return waitForServeShutdown(restartRequests, httpErrors, shutdownServers)
}

// newAPIServer wires the high-level server.Server with options, runtime
// dependencies, and the restart-request signalling channel. Returns an
// error if server.New fails to initialise its boot-time secrets (Plan
// 3 Task 4 / Q-7) instead of relying on the previous panic-recovery
// path.
func newAPIServer(
	options serveConfig,
	store storage.Store,
	logger *slog.Logger,
	panelRuntime server.PanelRuntime,
	restartRequests chan<- struct{},
) (*server.Server, error) {
	// The Prometheus /metrics scrape endpoint is a silent opt-in: when the
	// operator sets PANVEX_METRICS_SCRAPE_TOKEN the route is registered and
	// requires that bearer token; when unset, nothing is exposed. Accepted
	// only from the environment — never a CLI flag — so the secret does not
	// leak into process listings or shell history.
	metricsScrapeToken := strings.TrimSpace(os.Getenv("PANVEX_METRICS_SCRAPE_TOKEN"))

	// sqlitePath is non-empty only when the SQLite driver is selected.
	// The geoip subsystem uses it to derive a default storage directory
	// (<dir(SQLitePath)>/geoip) so .mmdb files live next to the DB; for
	// Postgres deployments it stays empty and geoip falls back to its
	// generic default (or PANVEX_GEOIP_DIR).
	var sqlitePath string
	if options.Storage.Driver == config.StorageDriverSQLite {
		sqlitePath = options.Storage.DSN
	}

	return server.New(server.Options{
		Now:                time.Now,
		Store:              store,
		Logger:             logger,
		UIFiles:            embeddedUIFiles(),
		PanelRuntime:       panelRuntime,
		TrustedProxyCIDRs:  options.TrustedProxyCIDRs,
		EncryptionKey:      options.EncryptionKey,
		Version:            Version,
		CommitSHA:          CommitSHA,
		BuildTime:          BuildTime,
		MetricsScrapeToken: metricsScrapeToken,
		SQLitePath:         sqlitePath,
		Bootstrap:          options.Boot,
		BootstrapSources:   options.BootstrapSources,
		RequestRestart: func() error {
			select {
			case restartRequests <- struct{}{}:
			default:
			}
			return nil
		},
	})
}

func parseServeConfig(args []string) (serveConfig, error) {
	flags := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	configPath := flags.String("config", strings.TrimSpace(os.Getenv("PANVEX_CONFIG")), "Path to runtime config.toml (also PANVEX_CONFIG)")
	trustedProxyCIDRs := flags.String("trusted-proxy-cidrs", "", "Comma-separated trusted proxy CIDRs for X-Forwarded-For (e.g. 172.16.0.0/12,10.0.0.0/8). Overrides PANVEX_TRUSTED_PROXY_CIDRS.")
	// CA encryption passphrase is never accepted on the command line because
	// argv is visible in /proc/<pid>/cmdline and ps output. Sources, in priority
	// order: --encryption-key-stdin, --encryption-key-file, PANVEX_ENCRYPTION_KEY.
	encryptionKeyFile := flags.String("encryption-key-file", "", "Path to file containing the passphrase for CA private key encryption")
	encryptionKeyStdin := flags.Bool("encryption-key-stdin", false, "Read the CA private key encryption passphrase from stdin (single line)")
	logLevel := flags.String("log-level", "", "Log level: debug, info, warn, error. Overrides PANVEX_LOG_LEVEL.")
	logFile := flags.String("log-file", "", "Path to log file (mirrors stderr; appended). Overrides PANVEX_LOG_FILE.")
	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}

	// Build the env slice, injecting CLI overrides so LoadBootstrap sees
	// the final merged view. CLI flags win over env when explicitly set.
	env := os.Environ()
	if v := strings.TrimSpace(*logLevel); v != "" {
		env = appendEnvOverride(env, "PANVEX_LOG_LEVEL", v)
	}
	if v := strings.TrimSpace(*logFile); v != "" {
		env = appendEnvOverride(env, "PANVEX_LOG_FILE", v)
	}
	// -trusted-proxy-cidrs CLI wins over PANVEX_TRUSTED_PROXY_CIDRS env.
	if v := strings.TrimSpace(*trustedProxyCIDRs); v != "" {
		env = appendEnvOverride(env, "PANVEX_TRUSTED_PROXY_CIDRS", v)
	}

	boot, srcs, err := settings.LoadBootstrap(settings.LoaderInput{
		ConfigPath: strings.TrimSpace(*configPath),
		Env:        env,
	})
	if err != nil {
		return serveConfig{}, err
	}

	// -encryption-key-file / -encryption-key-stdin override PANVEX_ENCRYPTION_KEY
	// because they supply the secret from a non-argv, non-env source. When
	// neither flag is provided resolveEncryptionKey falls through to env, which
	// LoadBootstrap has already read — so we only override when a file/stdin
	// path was explicitly requested.
	encryptionKey, err := resolveEncryptionKey(*encryptionKeyFile, *encryptionKeyStdin)
	if err != nil {
		return serveConfig{}, err
	}
	if encryptionKey != "" {
		// File/stdin takes priority; boot already has PANVEX_ENCRYPTION_KEY
		// from env — replace it with the stronger source.
		boot.AuthEncryptionKey = encryptionKey
	} else {
		// No file/stdin — honour what LoadBootstrap resolved from env/default.
		encryptionKey = boot.AuthEncryptionKey
	}

	parsedCIDRs, err := parseCIDRList(boot.HTTPTrustedProxyCIDRs)
	if err != nil {
		return serveConfig{}, fmt.Errorf("invalid trusted_proxy_cidrs: %w", err)
	}

	panelAllowedCIDRs := splitCSV(boot.HTTPPanelAllowedCIDRs)

	storage, err := config.ResolveStorage(boot.StorageDriver, boot.StorageDSN)
	if err != nil {
		return serveConfig{}, err
	}

	configManagedRuntime := strings.TrimSpace(*configPath) != ""

	return serveConfig{
		ConfigPath:           strings.TrimSpace(*configPath),
		ConfigManagedRuntime: configManagedRuntime,
		HTTPAddr:             boot.HTTPListenAddress,
		HTTPRootPath:         boot.HTTPRootPath,
		AgentHTTPRootPath:    boot.HTTPAgentRootPath,
		PanelAllowedCIDRs:    panelAllowedCIDRs,
		GRPCAddr:             boot.GRPCListenAddress,
		RestartMode:          boot.PanelRestartMode,
		TLSMode:              boot.TLSMode,
		TLSCertFile:          boot.TLSCertFile,
		TLSKeyFile:           boot.TLSKeyFile,
		Storage:              storage,
		TrustedProxyCIDRs:    parsedCIDRs,
		EncryptionKey:        encryptionKey,
		LogLevel:             boot.ObservabilityLogLevel,
		LogFile:              boot.ObservabilityLogFile,
		Boot:                 boot,
		BootstrapSources:     srcs,
	}, nil
}

// appendEnvOverride appends KEY=VALUE to the env slice, overriding any
// existing value for KEY. LoadBootstrap uses the last entry for a given
// key (first-match is the loader's model), so append is sufficient.
func appendEnvOverride(env []string, key, value string) []string {
	// Remove any existing entry for this key so the override is unambiguous.
	prefix := key + "="
	out := env[:0:len(env)]
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}

// splitCSV splits a comma-separated string into a trimmed slice of
// non-empty tokens. Used to parse PANVEX_PANEL_ALLOWED_CIDRS.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func resolvePanelRuntime(configuration serveConfig) (server.PanelRuntime, error) {
	tlsMode := configuration.TLSMode
	if strings.TrimSpace(tlsMode) == "" {
		tlsMode = config.PanelTLSModeProxy
	}

	runtime := server.PanelRuntime{
		HTTPListenAddress: configuration.HTTPAddr,
		HTTPRootPath:      configuration.HTTPRootPath,
		GRPCListenAddress: configuration.GRPCAddr,
		TLSMode:           tlsMode,
		TLSCertFile:       configuration.TLSCertFile,
		TLSKeyFile:        configuration.TLSKeyFile,
		RestartSupported:  configuration.RestartMode == config.RestartModeSupervised,
		ConfigSource:      server.PanelRuntimeSourceLegacy,
		ConfigPath:        configuration.ConfigPath,
	}
	if configuration.ConfigManagedRuntime {
		runtime.ConfigSource = server.PanelRuntimeSourceConfigFile
	}

	runtime.AgentHTTPRootPath = configuration.AgentHTTPRootPath

	var panelCIDRs []*net.IPNet
	for _, cidrStr := range configuration.PanelAllowedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidrStr)
		if err != nil {
			return server.PanelRuntime{}, fmt.Errorf("invalid panel_allowed_cidrs entry %q: %w", cidrStr, err)
		}
		panelCIDRs = append(panelCIDRs, ipNet)
	}
	runtime.PanelAllowedCIDRs = panelCIDRs

	return runtime, nil
}

// startPprofListenerIfConfigured brings up the S-07 separate pprof listener
// when PANVEX_PPROF_ADDR is set. Returns the shutdown func from the listener
// (callers must invoke it during graceful shutdown) or nil when the feature
// is disabled. A bind error is fatal — the operator opted in expecting
// pprof to be available; silently falling back to "no pprof" hides the
// misconfiguration.
func startPprofListenerIfConfigured(api *server.Server) (func(context.Context) error, error) {
	if api == nil {
		return nil, nil
	}
	addr := strings.TrimSpace(os.Getenv("PANVEX_PPROF_ADDR"))
	if addr == "" {
		return nil, nil
	}
	_, shutdown, err := api.StartPprofListener(context.Background())
	if err != nil {
		return nil, fmt.Errorf("start pprof listener on %s: %w", addr, err)
	}
	return shutdown, nil
}
