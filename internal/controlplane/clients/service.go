package clients

// Service is the (currently stub) orchestration entry point for managed
// clients. It will eventually own the create/update/rotate/delete/adopt
// flows now living on controlplane/server.Server. For P3-ARCH-01b it
// exposes only the pure helpers as methods so callers that want to
// depend on an interface (e.g. for testing) already can.
//
// Stateful fields (store, jobs, logger, in-memory maps) will be added
// in the remaining P3-ARCH-01b slice; for now Service is intentionally
// zero-value usable.
type Service struct{}

// NewService returns a zero-value Service. The signature is kept
// intentionally trivial so the follow-up commit can add dependencies
// without breaking call sites that already request a Service.
func NewService() *Service {
	return &Service{}
}

// ResolveTargetAgentIDs is a method wrapper over the package-level
// pure helper. See ResolveTargetAgentIDs in resolver.go.
func (s *Service) ResolveTargetAgentIDs(assignments []Assignment, topology AgentTopology) []string {
	return ResolveTargetAgentIDs(assignments, topology)
}

// ResolveIDByName is a method wrapper over the package-level pure
// helper. See ResolveIDByName in resolver.go.
func (s *Service) ResolveIDByName(
	clients map[string]Client,
	assignmentsByClient map[string][]Assignment,
	agentID string,
	agentFleetGroupID string,
	clientName string,
) string {
	return ResolveIDByName(clients, assignmentsByClient, agentID, agentFleetGroupID, clientName)
}

// AggregateUsage is a method wrapper over the package-level pure
// helper. See AggregateUsage in resolver.go.
func (s *Service) AggregateUsage(usageByAgent map[string]UsageSnapshot) AggregatedUsage {
	return AggregateUsage(usageByAgent)
}

// ValidateHexSecret reports whether s is a 32-char hex string. Thin
// method wrapper so mock services can stub validation.
func (s *Service) ValidateHexSecret(secret string) bool {
	return IsValidHexSecret(secret)
}
