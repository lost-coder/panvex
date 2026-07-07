// Package agents holds the control-plane's agent-lifecycle domain model.
//
// This package is the first slice of the P3-ARCH-01 god-package split
// (remediation plan v4, task 01a). It currently exports:
//   - The SessionManager that multiplexes live gRPC stream sessions by
//     agent ID (replacing the ad-hoc map that used to live on
//     controlplane/server.Server).
//   - The LiveStore hot cache plus the ReachabilityTracker and
//     FallbackTracker that back agent presence/telemetry.
//   - Pure, I/O-free helpers that normalize a gatewayrpc.RuntimeSnapshot
//     into derived values (RuntimeLifecycleState).
//
// Orchestration (enrollment, snapshot application, HTTP handlers) still
// lives in controlplane/server for now; later P3-ARCH-01 tasks (01b/c/d)
// will migrate the orchestration in once supporting packages
// (authority, enrollment, audit) are also extracted.
package agents
