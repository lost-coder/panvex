// internal/controlplane/discovered/doc.go
//
// Package discovered owns the "pending review" client snapshots that
// agents publish during reconcile. Discovered clients are an inbound
// stream from agents — they become managed (clients.Client) only after
// an operator adoption flow.
//
// Boundary: this package owns the DiscoveredClient aggregate and its
// Repository contract. Adoption (discovered → managed client) is a
// use case in clients.Service, not here.
package discovered
