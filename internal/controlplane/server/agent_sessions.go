package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
)

// registerAgentSession installs a new gRPC stream session for agentID.
// cancelConn is the connection ctx cancel for the stream being registered —
// the SessionManager invokes it if this session is later superseded (B5).
func (s *Server) registerAgentSession(agentID string, cancelConn context.CancelFunc) (*agents.Session, func()) {
	return s.sessions.Register(agentID, cancelConn)
}

// notifyAgentSession wakes the session currently attached to agentID.
func (s *Server) notifyAgentSession(agentID string) {
	s.sessions.Notify(agentID)
}

// notifyAgentSessions wakes a de-duplicated batch of agent sessions.
func (s *Server) notifyAgentSessions(agentIDs []string) {
	s.sessions.NotifyMany(agentIDs)
}
