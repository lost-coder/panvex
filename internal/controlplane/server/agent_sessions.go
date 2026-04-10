package server

type agentStreamSession struct {
	sequence uint64
	wake     chan struct{}
	// done is closed when the session is forcefully terminated (e.g. deregister).
	// Senders must check done before writing to wake to avoid sending on a
	// channel whose consumer has already exited.
	done chan struct{}
}

func (s *Server) registerAgentSession(agentID string) (*agentStreamSession, func()) {
	s.sessionMu.Lock()
	s.sessionSeq++
	session := &agentStreamSession{
		sequence: s.sessionSeq,
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
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
	if session != nil {
		select {
		case <-session.done:
		case session.wake <- struct{}{}:
		default:
		}
	}
	s.sessionMu.RUnlock()
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
