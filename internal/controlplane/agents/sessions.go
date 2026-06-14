package agents

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
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

	// RegisteredAt is when this session was installed. Used to flag a
	// suspiciously fast replacement: a healthy reconnect needs at least one
	// backoff cycle, while two live agents sharing one agent_id (cloned VM,
	// copied state file) re-register within seconds of each other,
	// repeatedly (B5).
	RegisteredAt time.Time

	// cancelConn cancels the gRPC connection ctx that owns this session.
	// Supplied at Register time; invoked when the session is superseded by a
	// newer Register or force-terminated, so the displaced stream's
	// goroutines (receive loop, 5s dispatch ticker) exit immediately instead
	// of lingering until their own Recv fails. May be nil in tests.
	cancelConn context.CancelFunc

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

// sessionReplaceWarnWindow is the "this is not a normal reconnect" horizon:
// replacements arriving faster than this after the previous Register are
// logged at WARN as a possible duplicate agent identity.
const sessionReplaceWarnWindow = 40 * time.Second

// shouldWarnOnReplace reports whether replacing `previous` at `now` is
// suspiciously fast. Pure function for testability.
func shouldWarnOnReplace(previous *Session, now time.Time) bool {
	return now.Sub(previous.RegisteredAt) < sessionReplaceWarnWindow
}

// Register installs a new Session for agentID and returns it along with an
// unregister closure that only removes this exact session. Any previous
// session for the same agentID is force-terminated (Done closed + its
// connection ctx cancelled): exactly one live stream per agent_id (B5).
// cancelConn is the cancel of the connection ctx that owns the NEW session.
func (m *SessionManager) Register(agentID string, cancelConn context.CancelFunc) (*Session, func()) {
	m.mu.Lock()
	previous := m.sessions[agentID]
	m.seq++
	session := &Session{
		Sequence:     m.seq,
		Wake:         make(chan struct{}, 1),
		Done:         make(chan struct{}),
		RegisteredAt: time.Now(),
		cancelConn:   cancelConn,
	}
	m.sessions[agentID] = session
	m.mu.Unlock()

	if previous != nil {
		terminateSession(previous)
		if shouldWarnOnReplace(previous, session.RegisteredAt) {
			slog.Warn("agent session replaced within keepalive window — possible duplicate agent identity (cloned VM or copied state file)",
				"agent_id", agentID,
				"previous_session_age", session.RegisteredAt.Sub(previous.RegisteredAt).Round(time.Millisecond),
			)
		}
	}

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

// terminateSession closes Done and cancels the owning stream ctx. Callers
// must have already removed the session from the map under m.mu, which makes
// them the sole terminator — a double close cannot occur via the public API.
func terminateSession(s *Session) {
	if s.Done != nil {
		close(s.Done)
	}
	if s.cancelConn != nil {
		s.cancelConn()
	}
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

// Terminate force-closes the session for agentID (if any), removes it from
// the map and cancels its connection ctx so the stream tears down without
// waiting for its own Recv to fail. Returns true when a session was present.
func (m *SessionManager) Terminate(agentID string) bool {
	m.mu.Lock()
	session, ok := m.sessions[agentID]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.sessions, agentID)
	m.mu.Unlock()
	terminateSession(session)
	return true
}
