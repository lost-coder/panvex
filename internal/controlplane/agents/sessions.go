package agents

import (
	"sync"
	"sync/atomic"
)

// Session tracks one active bi-directional gRPC stream between the
// control-plane and an agent. Senders on the wake channel MUST first
// observe done being open to avoid writing on a channel whose consumer
// has already exited.
type Session struct {
	// Sequence is a per-process monotonic ID. It lets registration races
	// be resolved: when a stream disconnects, the unregister closure only
	// removes the session it actually installed, not a later replacement.
	Sequence uint64

	// Wake is signalled by the control-plane when there is work for the
	// stream's sender goroutine to push. Buffered (cap 1) — if a wake is
	// already queued the new one is dropped (coalescing).
	Wake chan struct{}

	// Done is closed to force-terminate the stream (e.g. operator deletes
	// the agent). Consumers must select on it alongside Wake.
	Done chan struct{}

	// rediscover is set by the control-plane to ask the stream's writer
	// goroutine to re-request a FULL_SNAPSHOT client list on its next wake,
	// without disturbing job dispatch. Consumed (and reset) via
	// TakeRediscovery so each request triggers exactly one re-discovery.
	rediscover atomic.Bool
}

// RequestRediscovery marks the session so the writer goroutine re-requests
// a full client list on its next wake. Idempotent — repeated calls before a
// TakeRediscovery coalesce into a single pending request.
func (s *Session) RequestRediscovery() { s.rediscover.Store(true) }

// TakeRediscovery atomically reports whether a re-discovery was requested and
// clears the flag. Returns false when nothing is pending.
func (s *Session) TakeRediscovery() bool { return s.rediscover.Swap(false) }

// SessionManager multiplexes gRPC stream sessions by agent ID. It
// replaces the ad-hoc sessionMu + agentSessions map that previously
// lived on controlplane/server.Server.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	seq      uint64
}

// NewSessionManager constructs a fresh, empty SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{sessions: make(map[string]*Session)}
}

// Register installs a new Session for agentID and returns it along with
// an unregister closure that only removes this exact session (so a
// concurrent reconnect that has already installed a newer session is
// not clobbered).
func (m *SessionManager) Register(agentID string) (*Session, func()) {
	m.mu.Lock()
	m.seq++
	session := &Session{
		Sequence: m.seq,
		Wake:     make(chan struct{}, 1),
		Done:     make(chan struct{}),
	}
	m.sessions[agentID] = session
	m.mu.Unlock()

	unregister := func() {
		m.mu.Lock()
		existing, ok := m.sessions[agentID]
		if ok && existing.Sequence == session.Sequence {
			delete(m.sessions, agentID)
		}
		m.mu.Unlock()
	}
	return session, unregister
}

// Notify wakes the session currently attached to agentID, if any.
// Sending on Wake is guarded by Done to avoid writing to a channel
// whose consumer has exited.
func (m *SessionManager) Notify(agentID string) {
	m.mu.RLock()
	session := m.sessions[agentID]
	if session != nil {
		select {
		case <-session.Done:
		case session.Wake <- struct{}{}:
		default:
		}
	}
	m.mu.RUnlock()
}

// NotifyMany is a de-duplicated Notify across a batch of agent IDs.
func (m *SessionManager) NotifyMany(agentIDs []string) {
	notified := make(map[string]struct{}, len(agentIDs))
	for _, agentID := range agentIDs {
		if _, seen := notified[agentID]; seen {
			continue
		}
		notified[agentID] = struct{}{}
		m.Notify(agentID)
	}
}

// RequestRediscovery sets the rediscovery flag on the session attached to
// agentID and wakes it. Returns true when a live session was found. The wake
// is guarded by Done, mirroring Notify, so it never writes to a channel whose
// consumer has already exited.
func (m *SessionManager) RequestRediscovery(agentID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session := m.sessions[agentID]
	if session == nil {
		return false
	}
	session.RequestRediscovery()
	select {
	case <-session.Done:
	case session.Wake <- struct{}{}:
	default:
	}
	return true
}

// RequestRediscoveryAll sets the rediscovery flag on every currently-attached
// session and wakes each one. Returns the number of sessions notified.
func (m *SessionManager) RequestRediscoveryAll() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, session := range m.sessions {
		session.RequestRediscovery()
		select {
		case <-session.Done:
		case session.Wake <- struct{}{}:
		default:
		}
		n++
	}
	return n
}

// Terminate force-closes the session for agentID (if any) and removes
// it from the map. Returns true when a session was present and closed.
// The done channel is closed so the stream's writer goroutine exits.
func (m *SessionManager) Terminate(agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[agentID]
	if !ok {
		return false
	}
	delete(m.sessions, agentID)
	if session.Done != nil {
		close(session.Done)
	}
	return true
}
