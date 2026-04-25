package main

import (
	"testing"
	"time"
)

// TestShutdownBudgetFitsGrace fails fast if a future change to the
// per-step budgets pushes total worst-case shutdown past the value
// documented for `terminationGracePeriodSeconds`. SIGKILL during the
// audit-event flush silently drops events the API already 200'd, so
// the constant in this file IS the contract with the deploy manifests.
func TestShutdownBudgetFitsGrace(t *testing.T) {
	const batchWriterDrain = 10 * time.Second
	const slack = 5 * time.Second
	want := httpShutdownBudget + grpcShutdownBudget + batchWriterDrain + slack
	if controlPlaneShutdownGraceMin < want {
		t.Fatalf("controlPlaneShutdownGraceMin=%v < required %v (http=%v + grpc=%v + batchWriter=%v + slack=%v)",
			controlPlaneShutdownGraceMin, want,
			httpShutdownBudget, grpcShutdownBudget, batchWriterDrain, slack)
	}
}
