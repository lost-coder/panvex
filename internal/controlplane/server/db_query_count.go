package server

import (
	"net/http"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// highQueryCountThreshold is the per-request DB-query budget above which we
// emit a structured WARN. The number is intentionally generous — a typical
// list endpoint touches the agent table once and the related instances
// table once, plus 1-2 lookups for the actor/role check, well under 10.
// Anything firing more than 30 round-trips for a single panel HTTP request
// is almost certainly an N+1 pattern (audit P-02). Operators page on the
// `alert=high_db_query_count` attribute the WARN carries.
const highQueryCountThreshold = 30

// dbQueryCountMiddleware attaches a fresh per-request query counter to ctx
// at the start of every panel HTTP request and emits a WARN at the end if
// the count exceeded highQueryCountThreshold. Storage backends increment
// the counter inside their ExecContext/QueryContext/QueryRowContext wrappers
// (see internal/controlplane/storage/{postgres,sqlite}/instrumented_executor.go).
//
// The middleware is mounted near the router root so every handler — panel,
// agent, /api, top-level — gets observability. The counter is no-op for any
// code path running outside this middleware (background batch writer, gRPC
// streams, startup hooks).
//
// Closes audit P-02: previously the audit suspected N+1 patterns in
// clients_flow / agent_flow but had no way to confirm without SQL tracing.
// The WARN gives operators an immediate paging signal; the per-request log
// line carries `path`, `method`, and `query_count` so dashboards can
// drill into the specific endpoint.
func (s *Server) dbQueryCountMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := storage.WithDBQueryCounter(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))

		count := storage.DBQueryCount(ctx)
		if count <= highQueryCountThreshold {
			return
		}
		if s.logger == nil {
			return
		}
		s.logger.Warn("high DB query count for single panel request",
			"path", r.URL.Path,
			"method", r.Method,
			"query_count", count,
			"threshold", highQueryCountThreshold,
			"alert", "high_db_query_count",
			"hint", "investigate for N+1 query pattern in the handler chain",
		)
	})
}
