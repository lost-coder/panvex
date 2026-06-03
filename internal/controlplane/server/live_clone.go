package server

// cloneAgentForMirror returns a deep copy of an Agent suitable for storage in
// (and retrieval from) agents.LiveStore. EVERY reference-type field of Agent
// and its embedded AgentRuntime must be cloned: a shallow struct copy aliases
// the slices/maps/pointers, which would let a handler mutate the mirror's
// backing arrays under the LiveStore lock and race a concurrent ApplySnapshot.
//
// Reference fields cloned here:
//   - Agent.CertIssuedAt, Agent.CertExpiresAt        (*time.Time)
//   - Agent.CertificateRecovery                      (*struct; request-time, normally nil in the mirror, cloned defensively)
//   - Runtime.ConnectionsBadByClass                  ([]ConnectionClassCount)
//   - Runtime.HandshakeFailuresByClass               ([]ConnectionClassCount)
//   - Runtime.DCs                                    ([]RuntimeDC)
//   - Runtime.Upstreams                              ([]RuntimeUpstream, each with []string Scopes)
//   - Runtime.RecentEvents                           ([]RuntimeEvent)
//   - Runtime.FallbackEnteredAtUnix                  (*int64)
//   - Runtime.MeWritersSummary                       (*RuntimeMeWritersSummary)
//
// All scalar fields are copied by the initial struct assignment.
func cloneAgentForMirror(a Agent) Agent {
	out := a // copies every scalar field by value

	if a.CertIssuedAt != nil {
		v := *a.CertIssuedAt
		out.CertIssuedAt = &v
	}
	if a.CertExpiresAt != nil {
		v := *a.CertExpiresAt
		out.CertExpiresAt = &v
	}
	if a.CertificateRecovery != nil {
		v := *a.CertificateRecovery
		out.CertificateRecovery = &v
	}

	out.Runtime = cloneAgentRuntimeForMirror(a.Runtime)
	return out
}

// cloneAgentRuntimeForMirror deep-copies the reference-type fields of an
// AgentRuntime. Split out so the clone inventory lives next to the type it
// mirrors. SystemLoad is all-scalar and is copied by the caller's struct
// assignment; only the slices and pointers below need explicit cloning.
func cloneAgentRuntimeForMirror(r AgentRuntime) AgentRuntime {
	out := r // copies scalars + the all-scalar RuntimeSystemLoad value

	if r.ConnectionsBadByClass != nil {
		out.ConnectionsBadByClass = append([]ConnectionClassCount(nil), r.ConnectionsBadByClass...)
	}
	if r.HandshakeFailuresByClass != nil {
		out.HandshakeFailuresByClass = append([]ConnectionClassCount(nil), r.HandshakeFailuresByClass...)
	}
	if r.DCs != nil {
		out.DCs = append([]RuntimeDC(nil), r.DCs...)
	}
	if r.RecentEvents != nil {
		out.RecentEvents = append([]RuntimeEvent(nil), r.RecentEvents...)
	}
	if r.Upstreams != nil {
		upstreams := make([]RuntimeUpstream, len(r.Upstreams))
		for i, u := range r.Upstreams {
			upstreams[i] = u
			if u.Scopes != nil {
				upstreams[i].Scopes = append([]string(nil), u.Scopes...)
			}
		}
		out.Upstreams = upstreams
	}
	if r.FallbackEnteredAtUnix != nil {
		v := *r.FallbackEnteredAtUnix
		out.FallbackEnteredAtUnix = &v
	}
	if r.MeWritersSummary != nil {
		v := *r.MeWritersSummary
		out.MeWritersSummary = &v
	}
	return out
}

// liveAgentMap materialises the live store's agents into a map keyed by agent
// id. The dashboard helpers (control-room / telemetry) take map[string]Agent;
// this adapts live.List (a slice of deep copies) to that shape. The returned
// map is caller-owned — the values are already isolated copies.
func (s *Server) liveAgentMap() map[string]Agent {
	list := s.live.List()
	out := make(map[string]Agent, len(list))
	for _, a := range list {
		out[a.ID] = a
	}
	return out
}

// liveInstanceMap materialises the live store's instances into a map keyed by
// instance id, for the same dashboard helpers.
func (s *Server) liveInstanceMap() map[string]Instance {
	list := s.live.AllInstances()
	out := make(map[string]Instance, len(list))
	for _, i := range list {
		out[i.ID] = i
	}
	return out
}

// cloneInstanceForMirror returns a deep copy of an Instance. Instance is
// currently all-scalar (verified against server/types.go), so a struct copy
// is a full deep copy; the function exists so any future reference field is
// cloned in exactly one place rather than silently aliasing the mirror.
func cloneInstanceForMirror(i Instance) Instance {
	return i
}
