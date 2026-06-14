package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
)

// agentStreamSession is kept as an alias so existing server-internal
// code that holds a *agentStreamSession reference continues to work.
// The concrete type lives in controlplane/agents (P3-ARCH-01a). The
// canonical field names on the alias are Wake/Done/Sequence (exported),
// which replaces the previously private wake/done/sequence fields.
type agentStreamSession = agents.Session

// registerAgentSession installs a new gRPC stream session for agentID.
// cancelConn is the connection ctx cancel for the stream being registered —
// the SessionManager invokes it if this session is later superseded (B5).
func (s *Server) registerAgentSession(agentID string, cancelConn context.CancelFunc) (*agentStreamSession, func()) {
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
