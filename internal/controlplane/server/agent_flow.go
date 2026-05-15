package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/security"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// tracer is the package-level OTel tracer used by control-plane hot
// paths (P3-OBS-01). It pulls from the global TracerProvider so when
// tracing is disabled at startup all Start/End calls are free.
var tracer = otel.Tracer("github.com/lost-coder/panvex/internal/controlplane/server")

type agentEnrollmentRequest struct {
	Token    string
	NodeName string
	Version  string
	// AttemptID is the enrollment.Recorder attempt opened by the HTTP
	// handler before this call. Optional — when empty, enrollAgent skips
	// timeline emission. Always set in the production HTTP path so the
	// per-step events line up with the right attempt row.
	AttemptID string
}

// enrollmentError carries an operator-friendly ErrorCode alongside the
// underlying cause. The HTTP handler wraps returns from enrollAgent in
// this so mapAndFailEnrollment can dispatch the right Recorder.Fail code
// without re-classifying the cause from a string match.
//
// Task 13 limits this to the success path (cert sign step) and a generic
// fall-through; Task 15 will tighten the negative-path classification
// alongside the new tests.
type enrollmentError struct {
	code   enrollment.ErrorCode
	cause  error
	fields map[string]any
}

func (e *enrollmentError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return string(e.code)
}

func (e *enrollmentError) Unwrap() error { return e.cause }

// classifyEnrollmentError maps an error returned by enrollAgent (or the
// surrounding HTTP handler) to the enrollment.ErrorCode the Recorder
// should report. Returns ErrInternal as a fall-through so the attempt
// still terminates with a code rather than lingering in in_progress.
//
// Task 15: token-expired and token-already-used now classify into their
// dedicated codes; the prior wiring routed both to ErrInternal because
// only the cert-sign path constructed a typed enrollmentError. The check
// uses errors.Is so a wrapped sentinel still classifies correctly.
func classifyEnrollmentError(err error) enrollment.ErrorCode {
	switch {
	case errors.Is(err, security.ErrEnrollmentTokenExpired):
		return enrollment.ErrTokenExpired
	case errors.Is(err, security.ErrEnrollmentTokenConsumed):
		return enrollment.ErrTokenAlreadyUsed
	case errors.Is(err, security.ErrEnrollmentTokenInvalid),
		errors.Is(err, errEnrollmentTokenRevoked):
		return enrollment.ErrTokenNotFound
	default:
		return enrollment.ErrInternal
	}
}

// mapAndFailEnrollment dispatches the appropriate enrollment.ErrorCode
// onto Recorder.Fail. Typed enrollmentError wins; otherwise the error is
// classified via classifyEnrollmentError so security sentinels surface
// as their dedicated codes instead of ErrInternal.
func (s *Server) mapAndFailEnrollment(ctx context.Context, attemptID string, err error) {
	if s.enrollmentRec == nil || attemptID == "" {
		return
	}
	var ee *enrollmentError
	if errors.As(err, &ee) {
		if failErr := s.enrollmentRec.Fail(ctx, attemptID, ee.code, ee.cause, ee.fields); failErr != nil {
			s.logger.Warn("enrollment.recorder fail", "attempt_id", attemptID, "error", failErr)
		}
		return
	}
	code := classifyEnrollmentError(err)
	if failErr := s.enrollmentRec.Fail(ctx, attemptID, code, err, nil); failErr != nil {
		s.logger.Warn("enrollment.recorder fail", "attempt_id", attemptID, "error", failErr)
	}
}

type agentEnrollmentResponse struct {
	AgentID        string
	CertificatePEM string
	PrivateKeyPEM  string
	CAPEM          string
	ExpiresAt      time.Time
}

type instanceSnapshot struct {
	ID                string
	Name              string
	Version           string
	ConfigFingerprint string
	ConnectedUsers    int
	ReadOnly          bool
}

type agentSnapshot struct {
	AgentID                  string
	NodeName                 string
	FleetGroupID             string
	Version                  string
	ReadOnly                 bool
	Instances                []instanceSnapshot
	Clients                  []clientUsageSnapshot
	HasClients               bool
	ClientIPs                []clientIPSnapshot
	HasClientIPs             bool
	Runtime                  *gatewayrpc.RuntimeSnapshot
	HasRuntime               bool
	RuntimeDiagnostics       *gatewayrpc.RuntimeDiagnosticsSnapshot
	RuntimeSecurityInventory *gatewayrpc.RuntimeSecurityInventorySnapshot
	Metrics                  map[string]uint64
	ObservedAt               time.Time
}

// clientUsageSnapshot now lives in controlplane/clients as
// UsageSnapshot. Kept as a server-local alias so the usage-aggregator
// hot path (applyClientUsageSnapshot, grpc_gateway.Connect,
// chaos_test.go) keeps compiling until the rename lands.
type clientUsageSnapshot = clients.UsageSnapshot

type clientIPSnapshot struct {
	ClientID  string
	ActiveIPs []string
}

func (s *Server) enrollAgent(ctx context.Context, request agentEnrollmentRequest, now time.Time) (agentEnrollmentResponse, error) {
	// P3-OBS-01: agent enrollment is a low-volume, high-value path
	// (token consumption + cert issuance + first DB write). Wrap it in
	// a custom span so operators can diagnose slow enrollments even
	// when the HTTP-level span is lost across reverse proxies.
	ctx, span := tracer.Start(ctx, "agents.enroll")
	defer span.End()

	token, err := s.consumeEnrollmentTokenWithContext(ctx, request.Token, now)
	if err != nil {
		span.RecordError(err)
		return agentEnrollmentResponse{}, err
	}
	span.SetAttributes(
		attribute.String("panvex.node_name", request.NodeName),
		attribute.String("panvex.agent_version", request.Version),
		attribute.String("panvex.fleet_group_id", token.FleetGroupID),
	)
	if s.enrollmentRec != nil && request.AttemptID != "" {
		s.enrollmentRec.Event(ctx, request.AttemptID, enrollment.StepTokenValidated, enrollment.LevelInfo, "token consumed", map[string]any{
			"fleet_group_id": token.FleetGroupID,
		})
	}

	// Agent IDs are UUIDv7 instead of the old monotonic "agent_<N>" scheme
	// because the old counter was process-local and reset on CP restart,
	// which could re-issue an ID whose prior owner still held a valid 30-day
	// client certificate (P1-SEC-05 / C5 CN collision).
	id7, err := uuid.NewV7()
	if err != nil {
		span.RecordError(err)
		return agentEnrollmentResponse{}, fmt.Errorf("generate agent id: %w", err)
	}
	agentID := id7.String()
	span.SetAttributes(attribute.String("panvex.agent_id", agentID))

	// D-1: issue the cert FIRST so a failure here cannot leave a partial
	// DB row + in-memory entry + consumed token behind. Cert issuance is
	// in-memory crypto (ECDSA keygen + sign) with no IO, so the realistic
	// failure modes are RNG / OOM / misconfigured CA — fast-fail before
	// touching persistence.
	issued, err := s.authority.issueClientCertificate(agentID, now)
	if err != nil {
		span.RecordError(err)
		return agentEnrollmentResponse{}, &enrollmentError{
			code:  enrollment.ErrCertSignFailed,
			cause: fmt.Errorf("issue client certificate: %w", err),
		}
	}
	if s.enrollmentRec != nil && request.AttemptID != "" {
		s.enrollmentRec.Event(ctx, request.AttemptID, enrollment.StepCertSigned, enrollment.LevelInfo, "cert signed", map[string]any{
			"serial": issued.Serial,
		})
	}

	certIssuedAt := now.UTC()
	certExpiresAt := issued.ExpiresAt.UTC()
	agent := Agent{
		ID:            agentID,
		NodeName:      request.NodeName,
		FleetGroupID:  token.FleetGroupID,
		Version:       request.Version,
		LastSeenAt:    now.UTC(),
		CertIssuedAt:  &certIssuedAt,
		CertExpiresAt: &certExpiresAt,
		CertSerial:    issued.Serial,
	}

	if s.store != nil {
		// Fleet-group existence is resolved/auto-created at token-issue
		// time (see issueEnrollmentTokenWithContext), so by the time an
		// agent bootstraps with a consumed token the group row is
		// guaranteed to exist. If a group was deleted between issue and
		// bootstrap, the FK on agents.fleet_group_id surfaces the
		// failure here.
		if err := s.store.PutAgent(ctx, agentToRecord(agent)); err != nil {
			return agentEnrollmentResponse{}, err
		}
	}

	s.mu.Lock()
	s.agents[agentID] = agent
	s.mu.Unlock()

	if s.batchWriter != nil {
		s.batchWriter.agents.Enqueue(agentToRecord(agent))
	}
	// Q4.U-S-04: persist the freshly-issued cert serial as the
	// authoritative pin. PutAgent above does not write cert_serial
	// (see storage/{sqlite,postgres}/agents.go) so the dedicated update
	// is still required to populate the pin column. Best-effort — a
	// write failure is logged but does not roll back enrollment, since
	// the in-memory pin already protects this process lifetime.
	if s.store != nil {
		if err := s.store.UpdateAgentCertSerial(ctx, agentID, issued.Serial); err != nil {
			s.logger.Warn("persist agent cert serial failed", "agent_id", agentID, "error", err)
		}
	}

	s.appendAuditWithContext(ctx, agentID, "agents.enrolled", agentID, map[string]any{
		"node_name":      request.NodeName,
		"fleet_group_id": token.FleetGroupID,
	})
	s.events.Publish(eventbus.Event{
		Type: "agents.enrolled",
		Data: agent,
	})

	return agentEnrollmentResponse{
		AgentID:        agentID,
		CertificatePEM: issued.CertificatePEM,
		PrivateKeyPEM:  issued.PrivateKeyPEM,
		CAPEM:          issued.CAPEM,
		ExpiresAt:      issued.ExpiresAt,
	}, nil
}

func (s *Server) applyClientUsageSnapshot(ctx context.Context, agentID string, clients []clientUsageSnapshot) {
	applyTrafficDelta := s.shouldApplyClientUsageDelta(agentID, clients)

	seen, toPersist := s.mergeClientUsageBatch(agentID, clients, applyTrafficDelta)
	s.persistClientUsageRecords(ctx, toPersist)
	s.zeroLiveGaugesForUntouchedClients(agentID, seen)
}

// shouldApplyClientUsageDelta evaluates the seq-dedup rules (P2-LOG-06 / L-07)
// and returns whether traffic deltas in the batch should be accumulated.
// As a side effect it advances or rewinds s.lastUsageSeq for this agent.
//
// Dedup rules:
//   - seq == 0 on the wire: legacy agent, fall back to unconditional
//     accumulation (old behavior).
//   - seq <= lastSeen: duplicate / replay after stream reconnect — skip
//     deltas, but still refresh live gauges.
//   - lastSeen > 0 && seq == 1: agent restarted with zero-ed counters — treat
//     as baseline, skip deltas, just record new seq.
//   - otherwise: accept and accumulate.
func (s *Server) shouldApplyClientUsageDelta(agentID string, clients []clientUsageSnapshot) bool {
	batchSeq := firstNonZeroSeq(clients)
	if batchSeq == 0 {
		return true
	}
	lastSeen := s.lastUsageSeq[agentID]
	switch {
	case lastSeen > 0 && batchSeq == 1:
		// Agent restart: counters reset to zero on the agent side, so the
		// incoming "delta" is actually an absolute baseline. Skip addition to
		// avoid double-counting and rewind the CP-side cursor so subsequent
		// in-order deltas (seq 2, 3, ...) are accepted.
		s.lastUsageSeq[agentID] = 1
		return false
	case batchSeq <= lastSeen:
		// Duplicate or stale (in-flight retry, out-of-order reconnect). Live
		// gauges may still be refreshed below, but do not re-accumulate.
		return false
	default:
		s.lastUsageSeq[agentID] = batchSeq
		return true
	}
}

// firstNonZeroSeq returns the first usage.Seq encountered across the batch,
// or zero if every entry omits the field (legacy wire format).
func firstNonZeroSeq(clients []clientUsageSnapshot) uint64 {
	for _, usage := range clients {
		if usage.Seq != 0 {
			return usage.Seq
		}
	}
	return 0
}

// mergeClientUsageBatch updates s.clientUsage for every entry in the batch
// and returns the seen-set plus the list of records to persist write-through.
//
// Persisted backing lets clientUsage survive a panel restart — otherwise each
// adopted client would snap to zero bytes and wait for the next agent tick,
// which only carries a single polling interval worth of delta.
func (s *Server) mergeClientUsageBatch(agentID string, clients []clientUsageSnapshot, applyTrafficDelta bool) (map[string]struct{}, []storage.ClientUsageRecord) {
	seen := make(map[string]struct{}, len(clients))
	toPersist := make([]storage.ClientUsageRecord, 0, len(clients))
	for _, usage := range clients {
		seen[string(usage.ClientID)] = struct{}{}
		if s.clientUsage[string(usage.ClientID)] == nil {
			s.clientUsage[string(usage.ClientID)] = make(map[string]clientUsageSnapshot)
		}
		current := s.clientUsage[string(usage.ClientID)][agentID]
		current.ClientID = usage.ClientID
		if applyTrafficDelta {
			current.TrafficUsedBytes += usage.TrafficUsedBytes
		}
		current.UniqueIPsUsed = usage.UniqueIPsUsed
		current.ActiveTCPConns = usage.ActiveTCPConns
		current.ActiveUniqueIPs = usage.ActiveUniqueIPs
		current.QuotaUsedBytes = usage.QuotaUsedBytes
		current.QuotaLastResetUnix = usage.QuotaLastResetUnix
		current.ObservedAt = usage.ObservedAt
		s.clientUsage[string(usage.ClientID)][agentID] = current
		s.trackClientUsageOwnerLocked(string(usage.ClientID), agentID)
		toPersist = append(toPersist, storage.ClientUsageRecord{
			ClientID:         string(usage.ClientID),
			AgentID:          agentID,
			TrafficUsedBytes: current.TrafficUsedBytes,
			UniqueIPsUsed:    current.UniqueIPsUsed,
			ActiveTCPConns:   current.ActiveTCPConns,
			ActiveUniqueIPs:  current.ActiveUniqueIPs,
			LastSeq:          s.lastUsageSeq[agentID],
			ObservedAt:       current.ObservedAt,
		})
	}
	return seen, toPersist
}

// persistClientUsageRecords write-throughs the merged usage state to storage
// (when configured). The batch flushes in a single transaction via
// UpsertClientUsageBulk — the singular UpsertClientUsage is the slow path and
// is not used here. P-1 (sprint S-23 perf-critical): a 500-clients x 50-agents
// tick was issuing 25k single-row Exec calls; the bulk variant collapses
// that to a handful of multi-row INSERTs in one transaction.
//
// On error the whole batch is logged once. ON CONFLICT (client_id, agent_id)
// DO UPDATE preserves the per-row last-write-wins semantics the old loop had —
// duplicates within one batch collapse to the trailing entry.
func (s *Server) persistClientUsageRecords(ctx context.Context, toPersist []storage.ClientUsageRecord) {
	if len(toPersist) == 0 {
		return
	}
	if s.clientsSvc == nil {
		return // defensive — Server not fully wired (early init / test fixture)
	}
	batch := make([]clients.Usage, len(toPersist))
	for i, r := range toPersist {
		batch[i] = clients.Usage{
			ClientID:         clients.ClientID(r.ClientID),
			AgentID:          r.AgentID,
			TrafficUsedBytes: r.TrafficUsedBytes,
			UniqueIPsUsed:    r.UniqueIPsUsed,
			ActiveTCPConns:   r.ActiveTCPConns,
			ActiveUniqueIPs:  r.ActiveUniqueIPs,
			LastSeq:          r.LastSeq,
			ObservedAt:       r.ObservedAt,
		}
	}
	if err := s.clientsSvc.UpsertUsageBulk(ctx, batch); err != nil {
		s.logger.Warn("persist client_usage (bulk)", "rows", len(toPersist), "error", err)
	}
}

// zeroLiveGaugesForUntouchedClients zeros the live connection/IP gauges of
// any client that this agent did not include in the snapshot. Accumulated
// traffic is preserved — deleting the entry would lose the seeded/historical
// total.
//
// P-11: walks only the per-agent reverse index (agentClientUsage[agentID])
// rather than the full clientUsage map. With 5k clients × 50 agents, the
// old outer×inner scan was 250k iterations per snapshot ack under
// s.clientsMu; the delta-only walk visits at most "clients this agent
// has gauges for" per ack — typically a small fraction of the total.
//
// Caller must hold s.clientsMu (write).
func (s *Server) zeroLiveGaugesForUntouchedClients(agentID string, seen map[string]struct{}) {
	owned := s.agentClientUsage[agentID]
	if len(owned) == 0 {
		return
	}
	for clientID := range owned {
		if _, ok := seen[clientID]; ok {
			continue
		}
		usageByAgent := s.clientUsage[clientID]
		if usageByAgent == nil {
			// Defensive: if the forward map disagrees with the reverse
			// index, drop the stale reverse entry rather than crash.
			delete(owned, clientID)
			continue
		}
		current, ok := usageByAgent[agentID]
		if !ok {
			delete(owned, clientID)
			continue
		}
		current.ActiveTCPConns = 0
		current.ActiveUniqueIPs = 0
		usageByAgent[agentID] = current
	}
}

// agentTotalTrafficLocked sums TrafficUsedBytes across every client this
// agent has reported usage for. Used by the telemetry summary so the
// servers list can show real per-node traffic instead of synthetic
// placeholders. Caller MUST already hold s.clientsMu (read or write) —
// the function only reads the maps and never escalates the lock.
func (s *Server) agentTotalTrafficLocked(agentID string) uint64 {
	owned := s.agentClientUsage[agentID]
	if len(owned) == 0 {
		return 0
	}
	var total uint64
	for clientID := range owned {
		usage, ok := s.clientUsage[clientID][agentID]
		if !ok {
			continue
		}
		total += usage.TrafficUsedBytes
	}
	return total
}

// trackClientUsageOwnerLocked records that `agentID` owns a usage entry
// for `clientID` in s.clientUsage. Idempotent. Caller must hold
// s.clientsMu (write).
func (s *Server) trackClientUsageOwnerLocked(clientID, agentID string) {
	owned := s.agentClientUsage[agentID]
	if owned == nil {
		owned = make(map[string]struct{})
		s.agentClientUsage[agentID] = owned
	}
	owned[clientID] = struct{}{}
}

func (s *Server) applyClientIPSnapshot(agentID string, ipSnapshots []clientIPSnapshot) {
	for _, snapshot := range ipSnapshots {
		usageByAgent := s.clientUsage[snapshot.ClientID]
		if usageByAgent == nil {
			usageByAgent = make(map[string]clientUsageSnapshot)
			s.clientUsage[snapshot.ClientID] = usageByAgent
		}
		current := usageByAgent[agentID]
		current.ClientID = clients.ClientID(snapshot.ClientID)
		current.UniqueIPsUsed = len(snapshot.ActiveIPs)
		current.ActiveUniqueIPs = len(snapshot.ActiveIPs)
		usageByAgent[agentID] = current
		s.trackClientUsageOwnerLocked(snapshot.ClientID, agentID)
	}
}
