package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

// routes wires every HTTP endpoint into a chi router and returns the
// handler the public Server.Handler() exposes. Split out of server.go
// (R-Q-01/07) to keep the constructor + lifecycle code separate from
// the routing tree, which is the largest single piece of the
// control-plane HTTP surface.
//
// Layering (top → bottom):
//
//   - requestID + metrics + securityHeaders + bodySize: outermost so
//     every response (including 401/403/429/404) carries a request id
//     and is observed under a stable route pattern.
//   - per-request deadline: the chi-side timeout sits below the
//     transport WriteTimeout so the handler sees the cancellation
//     first and can emit a clean error.
//   - csrfOriginCheck: cheap origin/referer guard before any handler
//     runs.
//   - /healthz, /readyz, /metrics: registered at the top level
//     (outside /api) so the probes never depend on session cookies
//     and Prometheus does not need to learn about /api routing.
//
// The function intentionally hands the panel-facing tree, the agent
// tree, and the optional agent-prefix outer mux off to chi.Route so a
// reader can scan one block per role (agent / panel / authenticated /
// operator / admin) without context-switching across files.
func (s *Server) routes() http.Handler {
	router := chi.NewRouter()
	// metricsMiddleware must be the outermost user middleware so every
	// response — including 401s from ipWhitelist, 429s from rate-limiters,
	// and 404s from the UI fallback — is observed with its route pattern.
	// requestIDMiddleware runs first so every downstream middleware (incl.
	// metrics, logging, panics) can attribute its work to a stable
	// correlation ID. The ID is also echoed on the response.
	router.Use(requestIDMiddleware)
	router.Use(s.metricsMiddleware)
	router.Use(securityHeaders)
	router.Use(maxBodySize)
	// B8: per-request deadline for non-streaming handlers. Sits below
	// http.Server.WriteTimeout so the handler sees the cancellation
	// first and can emit a clean error instead of being torn down by
	// the transport.
	router.Use(requestTimeoutMiddleware(defaultRequestTimeout))
	router.Use(s.csrfOriginCheck(s.panelRuntime.HTTPRootPath, s.panelRuntime.AgentHTTPRootPath))
	router.Get("/healthz", handleHealthz())
	router.Get("/readyz", s.handleReadyz())
	// /metrics is registered at the top level (outside the /api group) so
	// Prometheus does not need session cookies. It is bearer-token gated in
	// handleMetrics; when no token is configured, the route is omitted.
	if s.metricsScrapeToken != "" {
		router.Method(http.MethodGet, "/metrics", s.handleScrapeMetrics(s.metricsScrapeToken))
	}

	panelPath := s.panelRuntime.HTTPRootPath
	agentPath := s.panelRuntime.AgentHTTPRootPath

	// Agent routes registered at apiBasePath (no whitelist).
	// When agentPath differs from panelPath, also register under the
	// separate agent prefix so they are reachable without stripRootPath.
	router.Route(apiBasePath, func(api chi.Router) {
		api.With(s.withRateLimit(s.agentBootstrapRateLimiter, "agent_bootstrap", s.requestClientRateLimitKey)).
			Post("/agent/bootstrap", s.handleAgentBootstrap())
		api.With(s.withRateLimit(s.agentBootstrapRateLimiter, "agent_bootstrap", s.requestClientRateLimitKey)).
			Post("/agent/recover-certificate", s.handleAgentCertificateRecovery())

		// Panel routes — with optional IP whitelist
		api.Group(func(panel chi.Router) {
			if len(s.panelRuntime.PanelAllowedCIDRs) > 0 {
				panel.Use(ipWhitelistMiddleware(s.panelRuntime.PanelAllowedCIDRs, s.trustedProxyCIDRs))
			}
			panel.With(s.withRateLimit(s.loginRateLimiter, "login", s.requestClientRateLimitKey)).
				Post("/auth/login", s.handleLogin())

			panel.Group(func(authenticated chi.Router) {
				authenticated.Use(s.requireAuthenticatedSession())
				// Phase-2 §2.5: double-submit CSRF check on state-changing
				// requests. Layered AFTER auth so the middleware has the
				// session.ID it needs to derive the expected token, and
				// BEFORE every state-changing handler in the chain.
				authenticated.Use(s.csrfTokenMiddleware)
				authenticated.Get("/version", s.handleVersion())
				authenticated.Get("/auth/me", s.handleMe())
				authenticated.Get("/auth/csrf-token", s.handleCSRFToken())
				authenticated.Post("/auth/logout", s.handleLogout())
				// Sensitive per-user rate limiting applied to any endpoint that
				// could be brute-forced (TOTP enable 6-digit code) or abused
				// at scale (enrollment token floods, repeated secret
				// rotations). Key is session.UserID, falling back to client IP.
				sensitive := s.withRateLimit(s.sensitiveRateLimiter, "sensitive", s.requestSessionRateLimitKey)
				authenticated.With(sensitive).Post("/auth/totp/setup", s.handleTotpSetup())
				authenticated.With(sensitive).Post("/auth/totp/enable", s.handleTotpEnable())
				authenticated.With(sensitive).Post("/auth/totp/disable", s.handleTotpDisable())
				authenticated.Get("/control-room", s.handleControlRoom())
				authenticated.Get("/fleet", s.handleFleet())
				authenticated.Get("/agents", s.handleAgents())
				authenticated.Get("/instances", s.handleInstances())
				authenticated.Get("/jobs", s.handleJobs())
				authenticated.Get("/audit", s.handleAudit())
				authenticated.Get("/metrics", s.handleMetrics())
				authenticated.Get("/events", s.handleEvents())
				authenticated.Get("/settings/appearance", s.handleGetUserAppearance())
				authenticated.Put("/settings/appearance", s.handlePutUserAppearance())
				authenticated.Get("/telemetry/dashboard", s.handleTelemetryDashboard())
				authenticated.Get("/telemetry/servers", s.handleTelemetryServers())
				authenticated.Get("/telemetry/servers/{id}", s.handleTelemetryServerDetail())
				authenticated.Post("/telemetry/servers/{id}/detail-boost", s.handleTelemetryServerDetailBoost())
				authenticated.Get("/telemetry/servers/{id}/history/load", s.handleServerLoadHistory())
				authenticated.Get("/telemetry/servers/{id}/history/dc", s.handleDCHealthHistory())
				authenticated.Get("/clients/{id}/history/ips", s.handleClientIPHistory())

				authenticated.Group(func(operator chi.Router) {
					operator.Use(s.requireMinimumRole(auth.RoleOperator))
					operator.Post("/jobs", s.handleCreateJob())
					operator.Get("/clients", s.handleClients())
					// Q2.U-S-11: rate-limit ALL mutating client endpoints (create,
					// update, delete, redeploy, adopt, ignore) at the same per-user
					// budget as the existing rotate-secret. Listing/read stays
					// unthrottled — operators routinely refresh the table.
					operator.With(sensitive).Post("/clients", s.handleCreateClient())
					operator.Get("/clients/{id}", s.handleClient())
					operator.With(sensitive).Put("/clients/{id}", s.handleUpdateClient())
					operator.With(sensitive).Delete("/clients/{id}", s.handleDeleteClient())
					operator.With(sensitive).Post("/clients/{id}/rotate-secret", s.handleRotateClientSecret())
					operator.With(sensitive).Post("/clients/{id}/redeploy", s.handleRedeployClient())
					operator.Get("/discovered-clients", s.handleDiscoveredClients())
					operator.With(sensitive).Post("/discovered-clients/{id}/adopt", s.handleAdoptDiscoveredClient())
					operator.With(sensitive).Post("/discovered-clients/{id}/ignore", s.handleIgnoreDiscoveredClient())
					operator.Post("/telemetry/servers/{id}/refresh-diagnostics", s.handleTelemetryServerRefreshDiagnostics())
					operator.Get("/fleet-groups", s.handleListFleetGroups())
					operator.Post("/fleet-groups", s.handleCreateFleetGroup())
					operator.Get("/fleet-groups/{id}", s.handleGetFleetGroup())
					operator.Patch("/fleet-groups/{id}", s.handleUpdateFleetGroup())
					operator.Get("/fleet-groups/{id}/deletion-preview", s.handleFleetGroupDeletionPreview())
					operator.Delete("/fleet-groups/{id}", s.handleDeleteFleetGroup())
					operator.Post("/fleet-groups/{id}/integrations", s.handleInstallFleetGroupIntegration())
					operator.Get("/fleet-groups/{id}/integrations/{integrationId}", s.handleGetFleetGroupIntegration())
					operator.Patch("/fleet-groups/{id}/integrations/{integrationId}", s.handleUpdateFleetGroupIntegration())
					operator.Delete("/fleet-groups/{id}/integrations/{integrationId}", s.handleDeleteFleetGroupIntegration())
					operator.Get("/integration-kinds", s.handleListIntegrationKinds())
					operator.Get("/integration-provider-kinds", s.handleListProviderKinds())
					operator.Get("/integration-providers", s.handleListIntegrationProviders())
					operator.Post("/integration-providers", s.handleCreateIntegrationProvider())
					operator.Get("/integration-providers/{id}", s.handleGetIntegrationProvider())
					operator.Patch("/integration-providers/{id}", s.handleUpdateIntegrationProvider())
					operator.Delete("/integration-providers/{id}", s.handleDeleteIntegrationProvider())
					operator.Patch("/agents/{id}", s.handleRenameAgent())
					operator.Get("/agents/enrollment-tokens", s.handleListEnrollmentTokens())
					operator.With(sensitive).Post("/agents/enrollment-tokens", s.handleCreateEnrollmentToken())
					operator.With(sensitive).Post("/agents/enrollment-tokens/{value}/revoke", s.handleRevokeEnrollmentToken())
					operator.With(sensitive).Post("/agents/{id}/update", s.handleAgentUpdate())
					operator.Get("/agent/update/binary", s.handleAgentBinaryProxy())
				})

				authenticated.Group(func(admin chi.Router) {
					admin.Use(s.requireMinimumRole(auth.RoleAdmin))
					// P3-OBS-02: /debug/pprof/* is admin-only. The enclosing
					// authentication + role middleware ensures operators and
					// viewers receive 403 without ever reaching the profiler.
					registerPprofRoutes(admin)
					admin.Get("/users", s.handleUsers())
					admin.With(sensitive).Post("/users", s.handleCreateUser())
					admin.With(sensitive).Put("/users/{id}", s.handleUpdateUser())
					admin.With(sensitive).Delete("/users/{id}", s.handleDeleteUser())
					admin.With(sensitive).Post("/users/{id}/totp/reset", s.handleResetUserTotp())
					admin.With(sensitive).Post("/agents/{id}/certificate-recovery-grants", s.handleCreateAgentCertificateRecoveryGrant())
					admin.With(sensitive).Post("/agents/{id}/certificate-recovery-grants/revoke", s.handleRevokeAgentCertificateRecoveryGrant())
					admin.With(sensitive).Delete("/agents/{id}", s.handleDeregisterAgent())
					admin.Get("/settings/panel", s.handleGetPanelSettings())
					admin.Put("/settings/panel", s.handlePutPanelSettings())
					admin.With(sensitive).Post("/settings/panel/restart", s.handleRestartPanel())
					admin.Get("/settings/retention", s.handleGetRetentionSettings())
					admin.Put("/settings/retention", s.handlePutRetentionSettings())
					admin.Get("/settings/updates", s.handleGetUpdateSettings())
					admin.Put("/settings/updates", s.handlePutUpdateSettings())
					admin.With(sensitive).Post("/settings/updates/check", s.handleForceUpdateCheck())
					admin.With(sensitive).Post("/settings/panel/update", s.handlePanelUpdate())
				})
			})
		})
	})

	if uiHandler := newUIHandler(s.uiFiles, panelPath); uiHandler != nil {
		router.NotFound(uiHandler)
	}

	// When agentPath is separate from panelPath, create an outer mux that
	// routes agent-prefixed requests to the agent endpoints directly and
	// everything else through the normal stripRootPath pipeline.
	if agentPath != "" && agentPath != panelPath {
		outer := chi.NewRouter()
		outer.Use(securityHeaders)
		outer.Use(maxBodySize)
		outer.Use(s.csrfOriginCheck(s.panelRuntime.HTTPRootPath, s.panelRuntime.AgentHTTPRootPath))
		outer.Route(agentPath+apiBasePath, func(agentAPI chi.Router) {
			agentAPI.With(s.withRateLimit(s.agentBootstrapRateLimiter, "agent_bootstrap", s.requestClientRateLimitKey)).
				Post("/agent/bootstrap", s.handleAgentBootstrap())
			agentAPI.With(s.withRateLimit(s.agentBootstrapRateLimiter, "agent_bootstrap", s.requestClientRateLimitKey)).
				Post("/agent/recover-certificate", s.handleAgentCertificateRecovery())
		})
		if panelPath != "" {
			outer.NotFound(stripRootPath(panelPath, router))
		} else {
			outer.NotFound(router.ServeHTTP)
		}
		return outer
	}

	if panelPath == "" {
		return router
	}

	return stripRootPath(panelPath, router)
}
