// Package storage exposes a request-scoped DB query counter so HTTP
// middleware can observe the number of database round-trips a single panel
// request fires. Closes audit P-02: the audit suspected N+1 patterns in
// `clients_flow.go` / `agent_flow.go` but had no way to confirm without
// SQL tracing. This file plus instrumentedExecutor.go in each backend
// provides that confirmation surface.
package storage

import (
	"context"
	"sync/atomic"
)

type dbQueryCountKey struct{}

// WithDBQueryCounter installs a fresh atomic counter onto ctx. Call once at
// the start of every operation whose query count you want to measure
// (typically the HTTP middleware on each inbound request). Returns a derived
// ctx that downstream storage calls can read.
func WithDBQueryCounter(ctx context.Context) context.Context {
	return context.WithValue(ctx, dbQueryCountKey{}, new(atomic.Int64))
}

// IncrementDBQuery is called by instrumented dbExecutor wrappers on every
// query (Exec/Query/QueryRow). When ctx carries no counter (operations
// outside a tracked HTTP request — startup, batch writer, background
// jobs), this is a cheap no-op.
func IncrementDBQuery(ctx context.Context) {
	if ctx == nil {
		return
	}
	if c, ok := ctx.Value(dbQueryCountKey{}).(*atomic.Int64); ok {
		c.Add(1)
	}
}

// DBQueryCount returns the number of queries seen since WithDBQueryCounter
// was called on ctx. Returns 0 when no counter is attached.
func DBQueryCount(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	c, ok := ctx.Value(dbQueryCountKey{}).(*atomic.Int64)
	if !ok {
		return 0
	}
	return c.Load()
}
