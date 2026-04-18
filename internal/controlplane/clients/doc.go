// Package clients holds the control-plane's managed-client domain model.
//
// This package is the second slice of the P3-ARCH-01 god-package split
// (remediation plan v4, task 01b). It currently exports:
//   - Domain types: Client, Assignment, Deployment, DiscoveredRecord —
//     the pure-data DTOs mirrored by the in-memory and persisted state.
//   - Assignment-target constants (TargetTypeFleetGroup, TargetTypeAgent)
//     and deployment-status constants.
//   - Pure, I/O-free helpers: ResolveUserADTag, NormalizeExpiration,
//     IsValidHexSecret, RandomHexString, NormalizedIDs.
//   - Pure assignment-target resolver: ResolveTargetAgentIDs, which maps
//     a slice of assignments to the concrete set of agent IDs they
//     resolve to, given caller-supplied snapshots of the agent/fleet
//     topology.
//   - ResolveIDByName: pure lookup helper that resolves a managed client
//     ID from (agent, clientName) given caller-supplied snapshots.
//   - A stub Service struct that will eventually own managed-client
//     orchestration (create/update/rotate/delete/adopt); it currently
//     exposes only the pure helpers as methods so callers that want to
//     depend on an interface already can.
//
// Orchestration (state mutation, job dispatch, HTTP handlers, discovery
// reconcile) still lives in controlplane/server for now; the remaining
// P3-ARCH-01b work (deferred) will migrate the stateful methods in once
// the in-memory stores on controlplane/server.Server are extracted to
// their own package.
//
// The package has no knowledge of controlplane/server internals and
// must not import from it — it is intentionally a leaf dependency so
// every other package (including server) can depend on these types
// without introducing cycles.
package clients
