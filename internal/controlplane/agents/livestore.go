package agents

import "sync"

// LiveStore is the in-memory owner of the FULL live state of every agent:
// the agent's presentation value (identity + runtime telemetry) plus that
// agent's set of Telemt instances, with replace-semantics on the instance
// set. It is the service-side home for what the server today keeps in
// s.agents and s.instances (audit finding A2: a single owner for live
// fleet state, off the Server struct).
//
// # Why generic
//
// The agent value the server keeps (server.Agent) is a PRESENTATION type:
// 50+ JSON-tagged runtime fields, attached helpers (FailRatePct5mPtr /
// SetFailRatePct5m), and request-time-only fields (CertificateRecovery,
// PresenceState) the server computes per HTTP request. The agents package
// must not import server (server imports agents -> import cycle), and
// duplicating that whole presentation tree into a domain struct here would
// (a) clone 50+ fields across six nested structs that must then track the
// Telemt wire format in two places forever, and (b) force a domain ->
// presentation re-map on every dashboard request and WebSocket tick (the
// hot path). None of those presentation fields embed a server-only type,
// so the only thing the domain split would buy is a re-map tax.
//
// Instead LiveStore is generic over the agent value type A. The server
// instantiates LiveStore[server.Agent] and keeps its presentation types
// where they belong; LiveStore owns the MECHANICS — replace-semantics,
// per-agent instance prune, deep-copy isolation, eviction, and the mutex
// — which is the part A2 actually moves off the Server struct. Deep copy
// of the (server-defined) value types is supplied by the caller via clone
// funcs at construction, because LiveStore cannot know the shape of A's or
// the instance's reference-type fields.
//
// # Lock discipline
//
// LiveStore.mu protects both maps. The store owns its own mutex and never
// reaches into Server.mu (mirroring agents.Service / clients.Service), so
// the documented control-plane lock ordering is preserved.
type LiveStore[A any, I any] struct {
	cloneAgent    func(A) A
	cloneInstance func(I) I
	instanceID    func(I) string
	instanceAgent func(I) string

	mu        sync.RWMutex
	agents    map[string]A
	instances map[string]I
}

// NewLiveStore constructs an empty LiveStore.
//
// cloneAgent and cloneInstance MUST return deep copies of their argument
// (every reference-type field — slices, maps, pointers — cloned), because
// they are the only thing standing between the mirror and a handler that
// mutates a value the store returned. instanceID and instanceAgent project
// an instance's identity and owning-agent id; they drive the prune logic
// and per-agent lookups. All four funcs are required (nil panics at
// construction) so a miswired caller fails loudly at boot, not under a
// concurrent snapshot.
func NewLiveStore[A any, I any](
	cloneAgent func(A) A,
	cloneInstance func(I) I,
	instanceID func(I) string,
	instanceAgent func(I) string,
) *LiveStore[A, I] {
	switch {
	case cloneAgent == nil:
		panic("agents.NewLiveStore: cloneAgent is nil")
	case cloneInstance == nil:
		panic("agents.NewLiveStore: cloneInstance is nil")
	case instanceID == nil:
		panic("agents.NewLiveStore: instanceID is nil")
	case instanceAgent == nil:
		panic("agents.NewLiveStore: instanceAgent is nil")
	}
	return &LiveStore[A, I]{
		cloneAgent:    cloneAgent,
		cloneInstance: cloneInstance,
		instanceID:    instanceID,
		instanceAgent: instanceAgent,
		agents:        make(map[string]A),
		instances:     make(map[string]I),
	}
}

// ApplySnapshot is the hot-path write. It UNCONDITIONALLY overwrites the
// agent's full live value and REPLACES that agent's instance set, pruning
// any previously-known instances of THIS agent that are absent from the
// new set while leaving other agents' instances untouched. This mirrors
// the server's commitInstancesLocked prune (agent_snapshot.go).
//
// Unconditional (not gated on any DB-persist outcome) is deliberate: live
// state must reflect what the agent just reported even if persistence is
// async or fails — the Stage-1 lesson that the in-memory mirror is the
// source of truth for the read path, with the store catching up behind it.
//
// The agent value and every instance are deep-cloned on the way in, so the
// caller may retain and mutate the arguments after the call returns
// without aliasing the mirror.
func (s *LiveStore[A, I]) ApplySnapshot(agentID string, agent A, instances []I) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[agentID] = s.cloneAgent(agent)
	s.replaceInstancesLocked(agentID, instances)
}

// SetInstances replaces an agent's instance set with prune-semantics
// WITHOUT touching the agent value. The partial-snapshot path (IN-H6)
// re-commits the last-known instances while the agent record is updated
// separately, so this exists for callers that need the instance prune in
// isolation. Instances are deep-cloned on the way in.
func (s *LiveStore[A, I]) SetInstances(agentID string, instances []I) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceInstancesLocked(agentID, instances)
}

// replaceInstancesLocked prunes the agent's vanished instances then writes
// the new set. Caller must hold s.mu. Matches commitInstancesLocked.
func (s *LiveStore[A, I]) replaceInstancesLocked(agentID string, instances []I) {
	live := make(map[string]struct{}, len(instances))
	for _, inst := range instances {
		live[s.instanceID(inst)] = struct{}{}
	}
	for id, entry := range s.instances {
		if s.instanceAgent(entry) != agentID {
			continue
		}
		if _, ok := live[id]; ok {
			continue
		}
		delete(s.instances, id)
	}
	for _, inst := range instances {
		s.instances[s.instanceID(inst)] = s.cloneInstance(inst)
	}
}

// Get returns a deep copy of the agent's full live value. ok is false when
// the agent is not in the mirror.
func (s *LiveStore[A, I]) Get(agentID string) (A, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.agents[agentID]
	if !ok {
		var zero A
		return zero, false
	}
	return s.cloneAgent(agent), true
}

// List returns deep copies of every agent's full live value. Order is
// unspecified — callers that need ordering must sort.
func (s *LiveStore[A, I]) List() []A {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]A, 0, len(s.agents))
	for _, agent := range s.agents {
		out = append(out, s.cloneAgent(agent))
	}
	return out
}

// InstancesForAgent returns deep copies of the instances currently owned
// by agentID. Used by the partial-snapshot path (IN-H6) to read back the
// last-known instances. Order is unspecified.
func (s *LiveStore[A, I]) InstancesForAgent(agentID string) []I {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []I
	for _, inst := range s.instances {
		if s.instanceAgent(inst) == agentID {
			out = append(out, s.cloneInstance(inst))
		}
	}
	return out
}

// AllInstances returns deep copies of every instance across every agent.
// Used by the fleet-wide handlers (handleInstances / handleFleet) that
// iterate the whole instance set. Order is unspecified.
func (s *LiveStore[A, I]) AllInstances() []I {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]I, 0, len(s.instances))
	for _, inst := range s.instances {
		out = append(out, s.cloneInstance(inst))
	}
	return out
}

// Remove evicts the agent and all of its instances from the mirror. Other
// agents' instances are untouched.
func (s *LiveStore[A, I]) Remove(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, agentID)
	for id, entry := range s.instances {
		if s.instanceAgent(entry) == agentID {
			delete(s.instances, id)
		}
	}
}

// Has reports whether the agent is present in the live mirror. Provided for
// store completeness and test assertions; it is NOT used on the hot path. In
// particular it is not the snapshot resurrection guard — that lives in the
// server and keys off revokedAgentIDs, not live presence.
func (s *LiveStore[A, I]) Has(agentID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.agents[agentID]
	return ok
}

// Len reports the number of agents in the live mirror. Provided for store
// completeness and test assertions; not used on the hot path.
func (s *LiveStore[A, I]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.agents)
}
