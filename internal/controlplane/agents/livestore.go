package agents

import (
	"sync"

	"github.com/lost-coder/panvex/internal/controlplane/api"
)

// LiveStore is the in-memory owner of the FULL live state of every agent:
// the agent's presentation value (identity + runtime telemetry) plus that
// agent's set of Telemt instances, with replace-semantics on the instance
// set. It is the service-side home for what the server today keeps in
// s.agents and s.instances (audit finding A2: a single owner for live
// fleet state, off the Server struct).
//
// # Presentation values, injected clones
//
// The agent value is api.Agent, a PRESENTATION type: 50+ JSON-tagged runtime
// fields, attached helpers (FailRatePct5mPtr / SetFailRatePct5m), and
// request-time-only fields (CertificateRecovery, PresenceState) the server
// computes per HTTP request. LiveStore owns the MECHANICS — replace-semantics,
// per-agent instance prune, deep-copy isolation, eviction, and the mutex —
// which is the part A2 moves off the Server struct.
//
// LiveStore was generic (over the agent value type) until the presentation
// types moved to internal/controlplane/api (P8.2c): that broke the
// agents -> server import cycle which was the only reason for the generic, so
// the store is now concrete over api.Agent / api.Instance. Deep copy of the
// value types is still supplied by the caller via clone funcs at construction,
// because the reference-type field shapes (which slices/maps/pointers to copy)
// are a presentation concern the store should not hard-code.
//
// # Lock discipline
//
// LiveStore.mu protects both maps. The store owns its own mutex and never
// reaches into Server.mu (mirroring clients.Service), so
// the documented control-plane lock ordering is preserved.
type LiveStore struct {
	cloneAgent    func(api.Agent) api.Agent
	cloneInstance func(api.Instance) api.Instance
	instanceID    func(api.Instance) string

	mu     sync.RWMutex
	agents map[string]api.Agent
	// instances is a two-level index: agentID → instanceID → instance
	// (P6-6.2b, finding #13). Replace/lookup/remove for one agent touch
	// only that agent's inner map instead of scanning the whole fleet
	// under the exclusive lock. INVARIANT: every instance passed to
	// ApplySnapshot/SetInstances belongs to the agentID argument — the
	// owning agent is the OUTER KEY, not a field of the instance.
	instances map[string]map[string]api.Instance
}

// NewLiveStore constructs an empty LiveStore.
//
// cloneAgent and cloneInstance MUST return deep copies of their argument
// (every reference-type field — slices, maps, pointers — cloned), because
// they are the only thing standing between the mirror and a handler that
// mutates a value the store returned. instanceID projects an instance's
// identity; the owning agent is the outer key of the two-level index, not a
// field of the instance. All three funcs are required (nil panics at
// construction) so a
// miswired caller fails loudly at boot, not under a concurrent snapshot.
func NewLiveStore(
	cloneAgent func(api.Agent) api.Agent,
	cloneInstance func(api.Instance) api.Instance,
	instanceID func(api.Instance) string,
) *LiveStore {
	switch {
	case cloneAgent == nil:
		panic("agents.NewLiveStore: cloneAgent is nil")
	case cloneInstance == nil:
		panic("agents.NewLiveStore: cloneInstance is nil")
	case instanceID == nil:
		panic("agents.NewLiveStore: instanceID is nil")
	}
	return &LiveStore{
		cloneAgent:    cloneAgent,
		cloneInstance: cloneInstance,
		instanceID:    instanceID,
		agents:        make(map[string]api.Agent),
		instances:     make(map[string]map[string]api.Instance),
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
func (s *LiveStore) ApplySnapshot(agentID string, agent api.Agent, instances []api.Instance) {
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
func (s *LiveStore) SetInstances(agentID string, instances []api.Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceInstancesLocked(agentID, instances)
}

// replaceInstancesLocked REPLACES the agent's instance set: with the
// two-level index the prune of vanished instances is implicit — the old
// inner map is dropped wholesale. O(len(instances)), fleet-independent.
// Caller must hold s.mu.
func (s *LiveStore) replaceInstancesLocked(agentID string, instances []api.Instance) {
	if len(instances) == 0 {
		delete(s.instances, agentID)
		return
	}
	inner := make(map[string]api.Instance, len(instances))
	for _, inst := range instances {
		inner[s.instanceID(inst)] = s.cloneInstance(inst)
	}
	s.instances[agentID] = inner
}

// Get returns a deep copy of the agent's full live value. ok is false when
// the agent is not in the mirror.
func (s *LiveStore) Get(agentID string) (api.Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.agents[agentID]
	if !ok {
		var zero api.Agent
		return zero, false
	}
	return s.cloneAgent(agent), true
}

// List returns deep copies of every agent's full live value. Order is
// unspecified — callers that need ordering must sort.
func (s *LiveStore) List() []api.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]api.Agent, 0, len(s.agents))
	for _, agent := range s.agents {
		out = append(out, s.cloneAgent(agent))
	}
	return out
}

// InstancesForAgent returns deep copies of the instances currently owned
// by agentID. Used by the partial-snapshot path (IN-H6) to read back the
// last-known instances. Order is unspecified.
func (s *LiveStore) InstancesForAgent(agentID string) []api.Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inner := s.instances[agentID]
	if len(inner) == 0 {
		return nil
	}
	out := make([]api.Instance, 0, len(inner))
	for _, inst := range inner {
		out = append(out, s.cloneInstance(inst))
	}
	return out
}

// AllInstances returns deep copies of every instance across every agent.
// Used by the fleet-wide handlers (handleInstances / handleFleet) that
// iterate the whole instance set. Order is unspecified.
func (s *LiveStore) AllInstances() []api.Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total int
	for _, inner := range s.instances {
		total += len(inner)
	}
	out := make([]api.Instance, 0, total)
	for _, inner := range s.instances {
		for _, inst := range inner {
			out = append(out, s.cloneInstance(inst))
		}
	}
	return out
}

// Remove evicts the agent and all of its instances from the mirror. Other
// agents' instances are untouched.
func (s *LiveStore) Remove(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, agentID)
	delete(s.instances, agentID)
}

// Has reports whether the agent is present in the live mirror. Provided for
// store completeness and test assertions; it is NOT used on the hot path. In
// particular it is not the snapshot resurrection guard — that lives in the
// server and keys off revokedAgentIDs, not live presence.
func (s *LiveStore) Has(agentID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.agents[agentID]
	return ok
}

// Len reports the number of agents in the live mirror. Provided for store
// completeness and test assertions; not used on the hot path.
func (s *LiveStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.agents)
}
