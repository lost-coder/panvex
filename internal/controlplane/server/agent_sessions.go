package server

import "github.com/lost-coder/panvex/internal/controlplane/agents"

// agentStreamSession is kept as an alias so existing server-internal
// code that holds a *agentStreamSession reference continues to work.
// The concrete type lives in controlplane/agents (P3-ARCH-01a). The
// canonical field names on the alias are Wake/Done/Sequence (exported),
// which replaces the previously private wake/done/sequence fields.
type agentStreamSession = agents.Session

// registerAgentSession installs a new gRPC stream session for agentID.
// Thin adapter over Server.sessions (*agents.SessionManager).
func (s *Server) registerAgentSession(agentID string) (*agentStreamSession, func()) {
	return s.sessions.Register(agentID)
}

// notifyAgentSession wakes the session currently attached to agentID.
func (s *Server) notifyAgentSession(agentID string) {
	s.sessions.Notify(agentID)
}

// notifyAgentSessions wakes a de-duplicated batch of agent sessions.
func (s *Server) notifyAgentSessions(agentIDs []string) {
	s.sessions.NotifyMany(agentIDs)
}
