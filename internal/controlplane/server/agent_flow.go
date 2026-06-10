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
	// CSRPEM is the PEM-encoded CERTIFICATE REQUEST generated locally by
	// the agent. A9: the panel signs it; the private key never crosses the
	// wire. Required for inbound HTTP enrollment.
	CSRPEM string
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
	CAPEM          string
	ExpiresAt      time.Time
}

type instanceSnapshot struct {
	ID                string
	Name              string
	Version           string
	ConfigFingerprint string
	ManagedConfigHash string
	ManagedConfigJSON string
	Connections       int
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
	// Partial=true when the agent could not collect a full telemt snapshot;
	// the panel preserves last-known version/connections/read_only/uptime
	// rather than overwriting them with blanks (IN-H6).
	Partial bool
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

	token, err := s.peekEnrollmentTokenWithContext(ctx, request.Token, now)
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
		s.enrollmentRec.Event(ctx, request.AttemptID, enrollment.StepTokenValidated, enrollment.LevelInfo, "token validated", map[string]any{
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

	// D-1: issue the cert FIRST (outside the enrollment tx) so that a cert
	// failure never holds an open DB transaction. Cert issuance is pure
	// in-memory crypto (ECDSA sign) with no IO. The atomicity guarantee —
	// no consumed token without a matching agent row — is provided by the
	// Transact block below (C2).
	//
	// A9: the agent generated the keypair locally; we sign its CSR. The
	// agentID is minted server-side a moment ago, so the CSR's CN cannot
	// match it — requireCNMatch=false and the template CN (agentID) wins.
	issued, err := s.authority.issueAgentCertificateFromCSR(request.CSRPEM, agentID, agentCertificateLifetime, false, now)
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

	// C2: consume the token, create the agent row, and persist the cert
	// serial pin in ONE transaction — AFTER cert issuance succeeded.
	// Ordering constraints:
	//   - cert issuance stays OUTSIDE the tx (D-1): pure in-memory
	//     crypto, must not hold a DB transaction open;
	//   - the token is burned only together with a successful agent row,
	//     so a PutAgent failure can no longer strand a consumed token;
	//   - a concurrent enroll with the same token loses the
	//     ConsumeEnrollmentToken race inside the tx (storage.ErrConflict)
	//     and rolls back its own agent row.
	// Fleet-group existence is resolved/auto-created at token-issue time
	// (see issueEnrollmentTokenWithContext); a group deleted between
	// issue and bootstrap surfaces via the agents.fleet_group_id FK here.
	if err := s.store.Transact(ctx, func(tx storage.Store) error {
		if _, err := tx.ConsumeEnrollmentToken(ctx, request.Token, now.UTC()); err != nil {
			return err
		}
		if err := tx.PutAgent(ctx, agentToRecord(agent)); err != nil {
			return err
		}
		// Q4.U-S-04: PutAgent intentionally does not write cert_serial
		// (see storage/{sqlite,postgres}/agents.go); the dedicated
		// update populates the pin column inside the same tx, so the
		// pin can no longer be silently lost (was best-effort).
		return tx.UpdateAgentCertSerial(ctx, agentID, issued.Serial)
	}); err != nil {
		span.RecordError(err)
		if errors.Is(err, storage.ErrConflict) {
			return agentEnrollmentResponse{}, s.resolveConsumeConflict(ctx, request.Token)
		}
		if errors.Is(err, storage.ErrNotFound) {
			return agentEnrollmentResponse{}, security.ErrEnrollmentTokenInvalid
		}
		return agentEnrollmentResponse{}, err
	}

	// Best-effort: persist the SPKI pin outside the tx (fail-closed prereq, A1).
	s.persistAgentCertPin(ctx, agentID, issued.CertificatePEM)

	// Enrollment writes a fresh agent with no instances yet; ApplySnapshot
	// with a nil instance set establishes the live-state baseline. No s.mu
	// needed — the live store has its own lock.
	s.live.ApplySnapshot(agentID, agent, nil)

	if s.batchWriter != nil {
		s.batchWriter.agents.Enqueue(agentToRecord(agent))
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
		CAPEM:          issued.CAPEM,
		ExpiresAt:      issued.ExpiresAt,
	}, nil
}

func (s *Server) applyClientUsageSnapshot(ctx context.Context, agentID string, clients []clientUsageSnapshot) {
	applyTrafficDelta := s.shouldApplyClientUsageDelta(agentID, clients)

	seen, toPersist := s.mergeClientUsageBatch(agentID, clients, applyTrafficDelta)
	// Phase 3 (reset-quota drift): when Telemt's reported quota_last_reset_unix
	// is ahead of the panel's recorded last_reset_epoch_secs for this
	// (client, agent), the operator reset out-of-band (e.g. raw
	// curl against Telemt) and the panel must adopt the newer value so
	// later ticks don't keep computing the same drift. The reverse
	// case (panel newer than Telemt) means a reset job we ran did not
	// stick on Telemt — surfaced at API-response time as a drift flag,
	// not persisted here.
	deploymentsToPersist := s.advanceDeploymentsFromTelemtReset(agentID, clients)
	s.persistClientUsageRecords(ctx, toPersist)
	s.persistDeploymentsAfterReset(ctx, deploymentsToPersist)
	// Zero the live connection/IP gauges of any client this agent did not
	// include in the snapshot. Accumulated traffic is preserved. Mirror-only
	// (no DB write) — the gauges are derived per-tick and never persisted.
	s.clientsSvc.ZeroLiveGaugesForAgent(agentID, seen)
}

// advanceDeploymentsFromTelemtReset scans the usage batch and, for each
// (client, agent) where Telemt's reported quota_last_reset_unix is
// strictly newer than the panel's recorded last_reset_epoch_secs,
// updates the in-memory deployment and returns the changed deployments
// for write-through. The changed deployments are written back to the
// clients.Service mirror (and DB) by persistDeploymentsAfterReset via
// PersistDeployment, so this function only reads the current value.
func (s *Server) advanceDeploymentsFromTelemtReset(agentID string, clients []clientUsageSnapshot) []managedClientDeployment {
	if len(clients) == 0 {
		return nil
	}
	var changed []managedClientDeployment
	for _, usage := range clients {
		if usage.QuotaLastResetUnix == 0 {
			continue
		}
		clientID := string(usage.ClientID)
		deployment, ok := s.clientsSvc.MirrorDeployment(clientID, agentID)
		if !ok {
			continue
		}
		if usage.QuotaLastResetUnix <= deployment.LastResetEpochSecs {
			continue
		}
		deployment.LastResetEpochSecs = usage.QuotaLastResetUnix
		deployment.UpdatedAt = usage.ObservedAt.UTC()
		changed = append(changed, deployment)
	}
	return changed
}

// persistDeploymentsAfterReset write-throughs deployment rows whose
// last_reset_epoch_secs was bumped to match Telemt. Errors are logged
// and swallowed — the next snapshot tick will retry. Empty slice is a
// no-op so the common no-drift path costs nothing extra.
func (s *Server) persistDeploymentsAfterReset(ctx context.Context, deployments []managedClientDeployment) {
	if len(deployments) == 0 || s.clientsSvc == nil {
		return
	}
	for _, d := range deployments {
		if err := s.clientsSvc.PersistDeployment(ctx, d); err != nil {
			s.logger.Error("client deployment last-reset persistence failed",
				"client_id", string(d.ClientID), "agent_id", d.AgentID,
				"last_reset_epoch_secs", d.LastResetEpochSecs, "error", err)
		}
	}
}

// shouldApplyClientUsageDelta evaluates the seq-dedup rules (P2-LOG-06 / L-07)
// and returns whether traffic deltas in the batch should be accumulated.
// As a side effect it advances or rewinds the agent's mirror usage-seq cursor.
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
	lastSeen := s.clientsSvc.MirrorLastUsageSeq(agentID)
	switch {
	case lastSeen > 0 && batchSeq == 1:
		// Agent restart: counters reset to zero on the agent side, so the
		// incoming "delta" is actually an absolute baseline. Skip addition to
		// avoid double-counting and rewind the CP-side cursor so subsequent
		// in-order deltas (seq 2, 3, ...) are accepted.
		s.clientsSvc.MirrorSetLastUsageSeq(agentID, 1)
		return false
	case batchSeq <= lastSeen:
		// Duplicate or stale (in-flight retry, out-of-order reconnect). Live
		// gauges may still be refreshed below, but do not re-accumulate.
		return false
	default:
		// IN-C1: a jump of more than one means at least one usage snapshot
		// (and its one-shot delta) was lost in transit. With the non-drop
		// enqueue fix this should be rare (agent-side outbound drop or a
		// genuine reconnect reorder); surface it on a stable alert key so any
		// residual traffic undercount is observable rather than silent.
		if lastSeen > 0 && batchSeq > lastSeen+1 {
			s.logger.Warn("client usage seq gap — usage delta(s) may have been lost",
				"agent_id", agentID,
				"last_seen", lastSeen,
				"received", batchSeq,
				"alert", "client_usage_seq_gap",
			)
		}
		s.clientsSvc.MirrorSetLastUsageSeq(agentID, batchSeq)
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

// mergeClientUsageBatch computes the new absolute usage counters for every
// entry in the batch (reading the current totals from the clients.Service
// mirror) and returns the seen-set plus the list of records to persist
// write-through.
//
// Persisted backing lets usage survive a panel restart — otherwise each
// adopted client would snap to zero bytes and wait for the next agent tick,
// which only carries a single polling interval worth of delta.
func (s *Server) mergeClientUsageBatch(agentID string, clients []clientUsageSnapshot, applyTrafficDelta bool) (map[string]struct{}, []storage.ClientUsageRecord) {
	seen := make(map[string]struct{}, len(clients))
	toPersist := make([]storage.ClientUsageRecord, 0, len(clients))
	lastSeq := s.clientsSvc.MirrorLastUsageSeq(agentID)
	for _, usage := range clients {
		seen[string(usage.ClientID)] = struct{}{}
		// Read the current absolute counters from the mirror so the traffic
		// delta accumulates onto the persisted total (mirror is the single
		// owner of usage state; the persist below — UpsertUsageBulk — writes
		// the new absolute value back).
		current, _ := s.clientsSvc.MirrorUsageEntryFor(string(usage.ClientID), agentID)
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
		toPersist = append(toPersist, storage.ClientUsageRecord{
			ClientID:           string(usage.ClientID),
			AgentID:            agentID,
			TrafficUsedBytes:   current.TrafficUsedBytes,
			UniqueIPsUsed:      current.UniqueIPsUsed,
			ActiveTCPConns:     current.ActiveTCPConns,
			ActiveUniqueIPs:    current.ActiveUniqueIPs,
			QuotaUsedBytes:     current.QuotaUsedBytes,
			QuotaLastResetUnix: current.QuotaLastResetUnix,
			LastSeq:            lastSeq,
			ObservedAt:         current.ObservedAt,
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
			ClientID:           clients.ClientID(r.ClientID),
			AgentID:            r.AgentID,
			TrafficUsedBytes:   r.TrafficUsedBytes,
			UniqueIPsUsed:      r.UniqueIPsUsed,
			ActiveTCPConns:     r.ActiveTCPConns,
			ActiveUniqueIPs:    r.ActiveUniqueIPs,
			QuotaUsedBytes:     r.QuotaUsedBytes,
			QuotaLastResetUnix: r.QuotaLastResetUnix,
			LastSeq:            r.LastSeq,
			ObservedAt:         r.ObservedAt,
		}
	}
	// IN-H1: bounded retry + loud alert. The previous bare Warn made a
	// persistence failure both invisible and lossy: client-usage deltas are
	// one-shot (the agent never resends them), so a write dropped under a
	// transient DB hiccup permanently undercounts traffic. Retry transient
	// errors a few times with short backoff (this runs on the snapshot-drain
	// goroutine, not the gRPC receive loop), and escalate to Error with a
	// stable alert key so operators can page on the loss.
	const maxUsagePersistAttempts = 3
	var err error
	for attempt := 1; ; attempt++ {
		if err = s.clientsSvc.UpsertUsageBulk(ctx, batch); err == nil {
			return
		}
		if attempt >= maxUsagePersistAttempts || classifyFlushError(err) == "persistent" || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(attempt) * 25 * time.Millisecond):
		}
	}
	s.logger.Error("persist client_usage (bulk) failed",
		"rows", len(toPersist),
		"error", err,
		"alert", "client_usage_persist_failed",
	)
}

// agentTotalTraffic sums TrafficUsedBytes across every client this agent has
// reported usage for. Used by the telemetry summary so the servers list can
// show real per-node traffic instead of synthetic placeholders. Reads from
// the clients.Service mirror (the single owner of usage state).
func (s *Server) agentTotalTraffic(agentID string) uint64 {
	return s.clientsSvc.MirrorAgentTotalTraffic(agentID)
}
