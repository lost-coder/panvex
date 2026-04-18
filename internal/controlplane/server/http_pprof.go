package server

import (
	"net/http"
	"net/http/pprof"

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
