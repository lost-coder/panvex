package server

// Test helpers for seeding / reading the agents.LiveStore that replaced the
// former server-owned s.agents / s.instances maps (A2/A1). Tests used to write
// server.agents[id] = Agent{...} directly; they now go through the live store
// via these helpers so the deep-copy + prune semantics match production.

// seedLiveAgent installs an agent into the live store with no instances,
// preserving any instances the agent already had. Equivalent to the old
// server.agents[id] = agent.
func (s *Server) seedLiveAgent(agent Agent) {
	s.live.ApplySnapshot(agent.ID, agent, s.live.InstancesForAgent(agent.ID))
}

// seedLiveInstance installs a single instance into the live store without
// disturbing other instances of the same agent. Equivalent to the old
// server.instances[id] = instance.
func (s *Server) seedLiveInstance(instance Instance) {
	existing := s.live.InstancesForAgent(instance.AgentID)
	replaced := false
	for i := range existing {
		if existing[i].ID == instance.ID {
			existing[i] = instance
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, instance)
	}
	s.live.SetInstances(instance.AgentID, existing)
}

// seedLiveAgentKeyed installs an agent under an explicit key (the old
// server.agents[key] = agent form, where the struct literal may omit ID).
// The key wins over any ID set in the struct so the live store's prune keying
// stays consistent.
func (s *Server) seedLiveAgentKeyed(id string, agent Agent) {
	agent.ID = id
	s.seedLiveAgent(agent)
}

// liveAgentGet returns a copy of the agent by id (the old server.agents[id]
// read form). The second result mirrors the comma-ok map read.
func (s *Server) liveAgentGet(id string) (Agent, bool) {
	return s.live.Get(id)
}

// liveAgent returns the agent by id (the old server.agents[id] expression
// form), or a zero Agent when absent — matching Go map-index semantics.
func (s *Server) liveAgent(id string) Agent {
	agent, _ := s.live.Get(id)
	return agent
}

// seedLiveInstanceKeyed installs an instance under an explicit key (the old
// server.instances[key] = instance form, where the struct may omit ID).
func (s *Server) seedLiveInstanceKeyed(id string, instance Instance) {
	instance.ID = id
	s.seedLiveInstance(instance)
}

// liveInstanceGet returns an instance by id (the old server.instances[id]
// comma-ok read form).
func (s *Server) liveInstanceGet(id string) (Instance, bool) {
	for _, inst := range s.live.AllInstances() {
		if inst.ID == id {
			return inst, true
		}
	}
	return Instance{}, false
}

// liveAgentsForTest returns the live store's agents as a map keyed by id, for
// tests that previously ranged over server.agents.
func (s *Server) liveAgentsForTest() map[string]Agent {
	return s.liveAgentMap()
}
