// Package agents holds the control-plane's agent-lifecycle domain model.
//
// This package is the first slice of the P3-ARCH-01 god-package split
// (remediation plan v4, task 01a). It currently exports:
//   - Snapshot payload types received from agents over the gRPC gateway
//     (Snapshot, InstanceSnapshot, ClientUsageSnapshot, ClientIPSnapshot).
//   - Enrollment request/response DTOs used by the enrollment handshake.
//   - The SessionManager that multiplexes live gRPC stream sessions by
//     agent ID (replacing the ad-hoc map that used to live on
//     controlplane/server.Server).
//   - Pure, I/O-free helpers that normalize a gatewayrpc.RuntimeSnapshot
//     into derived values (RuntimeLifecycleState, SystemLoadFromSnapshot,
//     MeWritersSummaryFromSnapshot).
//
// Orchestration (enrollment, snapshot application, HTTP handlers) still
// lives in controlplane/server for now; later P3-ARCH-01 tasks (01b/c/d)
// will migrate the orchestration in once supporting packages
// (authority, enrollment, audit) are also extracted.
package agents
