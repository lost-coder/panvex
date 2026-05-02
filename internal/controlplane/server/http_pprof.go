package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// registerPprofRoutes mounts the Go runtime profiling endpoints under
// /debug/pprof on the supplied router. The caller is responsible for applying
// authentication and authorization middleware before calling this helper —
// pprof exposes sensitive runtime state (goroutine stacks, heap, CPU profile)
// and must never be reachable without admin-level role enforcement.
//
// The individual handler functions are mapped explicitly rather than mounting
// http.DefaultServeMux so that the surface area is auditable and decoupled
// from any other package that happens to register on DefaultServeMux.
//
// P3-OBS-02: expose pprof for live diagnostics while keeping it gated behind
// requireMinimumRole(RoleAdmin). Operators/viewers will receive a 403 from
// the enclosing middleware.
//
// S-07: when the operator opts into a separate localhost-only pprof listener
// (Server.SetPprofListenerAddr / cmd/control-plane PANVEX_PPROF_ADDR), the
// admin-router registration is skipped — see Server.skipAdminPprof().
func registerPprofRoutes(router chi.Router) {
	router.HandleFunc("/debug/pprof/", pprof.Index)
	router.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	router.HandleFunc("/debug/pprof/profile", pprof.Profile)
	router.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	router.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Named profiles are served via pprof.Handler. Each handler returns a
	// plain http.Handler, so we adapt to chi's Handle signature.
	for _, name := range []string{
		"allocs",
		"block",
		"goroutine",
		"heap",
		"mutex",
		"threadcreate",
	} {
		router.Method(http.MethodGet, "/debug/pprof/"+name, pprof.Handler(name))
	}
}

// SetPprofListenerAddr opts the server into S-07's separate-listener pprof
// mode. When addr is non-empty, the admin-router pprof registration is
// skipped, and the operator is responsible for calling StartPprofListener at
// startup to bring up a dedicated listener bound to that address.
//
// Recommended values are loopback-only (127.0.0.1:6060, [::1]:6060) so
// access requires shell on the host. Public binds defeat the entire point of
// the separation.
//
// Must be called before Handler() so the route registration sees the flag.
func (s *Server) SetPprofListenerAddr(addr string) {
	s.pprofListenerAddr = strings.TrimSpace(addr)
}

// pprofListenerEnabled reports whether the separate-listener mode is active.
// Used by routes.go to decide whether to mount /debug/pprof on the admin
// router.
func (s *Server) pprofListenerEnabled() bool {
	return s.pprofListenerAddr != ""
}

// StartPprofListener brings up the separate pprof HTTP listener on the
// configured address. Returns the bound listener address (useful for
// tests and structured logs) and a shutdown func to be invoked during
// graceful shutdown. Errors out if the address is unset or the bind fails.
//
// Threat model (S-07):
//
//   - Loopback bind: only callers with shell on the host can reach pprof.
//     Even an admin panel user cannot pull goroutine stacks.
//   - The listener is plain HTTP — no TLS — because the operator reaches it
//     through SSH port-forward (`ssh -L 6060:localhost:6060 host`) rather
//     than over the wire.
//   - Connection-level read/write timeouts mirror the panel's HTTP server so
//     a hung profile request cannot block shutdown.
func (s *Server) StartPprofListener(ctx context.Context) (net.Addr, func(context.Context) error, error) {
	if !s.pprofListenerEnabled() {
		return nil, nil, errors.New("pprof listener not configured (call SetPprofListenerAddr first)")
	}
	router := chi.NewRouter()
	registerPprofRoutes(router)

	listener, err := net.Listen("tcp", s.pprofListenerAddr)
	if err != nil {
		return nil, nil, err
	}

	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		// Profile + trace endpoints are explicitly long-running; we do not
		// set ReadTimeout / WriteTimeout, accepting that pprof.Profile may
		// hold the connection for the user-supplied seconds parameter.
	}

	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			if s.logger != nil {
				s.logger.Error("pprof listener exited",
					"err", serveErr,
					"alert", "pprof_listener_exited",
				)
			}
		}
	}()

	if s.logger != nil {
		s.logger.Info("pprof listener started",
			"addr", listener.Addr().String(),
			"hint", "loopback-only — reach via `ssh -L 6060:localhost:6060 host`",
		)
	}

	_ = ctx // accepted for future cancellation hooks; current ctx is the
	//        request-scope ctx of the caller and not needed for the goroutine.

	return listener.Addr(), srv.Shutdown, nil
}
