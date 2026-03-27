package server

type agentStreamSession struct {
	sequence uint64
	wake     chan struct{}
}

func (s *Server) registerAgentSession(agentID string) (*agentStreamSession, func()) {
	s.sessionMu.Lock()
	s.sessionSeq++
	session := &agentStreamSession{
		sequence: s.sessionSeq,
		wake:     make(chan struct{}, 1),
	}
	s.agentSessions[agentID] = session
	s.sessionMu.Unlock()

	unregister := func() {
		s.sessionMu.Lock()
		existing, ok := s.agentSessions[agentID]
		if ok && existing.sequence == session.sequence {
			delete(s.agentSessions, agentID)
		}
		s.sessionMu.Unlock()
	}

	return session, unregister
}

func (s *Server) notifyAgentSession(agentID string) {
	s.sessionMu.RLock()
	session := s.agentSessions[agentID]
	s.sessionMu.RUnlock()
	if session == nil {
		return
	}

	select {
	case session.wake <- struct{}{}:
	default:
	}
}

func (s *Server) notifyAgentSessions(agentIDs []string) {
	notified := make(map[string]struct{}, len(agentIDs))
	for _, agentID := range agentIDs {
		if _, seen := notified[agentID]; seen {
			continue
		}
		notified[agentID] = struct{}{}
		s.notifyAgentSession(agentID)
	}
}
