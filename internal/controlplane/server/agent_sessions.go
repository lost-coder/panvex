package server

// notifyAgentSessions wakes a de-duplicated batch of agent sessions. The
// single-agent Register/Notify calls belong to the gRPC stream and are
// issued by the gateway against agents.SessionManager directly.
func (s *Server) notifyAgentSessions(agentIDs []string) {
	s.sessions.NotifyMany(agentIDs)
}
