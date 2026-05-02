package server

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
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

	s.mu.Lock()
	agent := Agent{
		ID:           agentID,
		NodeName:     request.NodeName,
		FleetGroupID: token.FleetGroupID,
		Version:      request.Version,
		LastSeenAt:   now.UTC(),
	}
	s.mu.Unlock()

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

	issued, err := s.authority.issueClientCertificate(agentID, now)
	if err != nil {
		return agentEnrollmentResponse{}, err
	}

	// Record certificate dates in the agent so they are persisted and
	// returned via the API instead of being fabricated on the frontend.
	certIssuedAt := now.UTC()
	certExpiresAt := issued.ExpiresAt.UTC()
	s.mu.Lock()
	storedAgent := s.agents[agentID]
	storedAgent.CertIssuedAt = &certIssuedAt
	storedAgent.CertExpiresAt = &certExpiresAt
	storedAgent.CertSerial = issued.Serial
	s.agents[agentID] = storedAgent
	s.mu.Unlock()
	if s.batchWriter != nil {
		s.batchWriter.agents.Enqueue(agentToRecord(storedAgent))
	}
	// Q4.U-S-04: persist the freshly-issued cert serial as the
	// authoritative pin. Best-effort — a write failure is logged but
	// does not block enrollment, since the in-memory pin in
	// storedAgent already protects this process lifetime.
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
		Data: storedAgent,
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
		seen[usage.ClientID] = struct{}{}
		if s.clientUsage[usage.ClientID] == nil {
			s.clientUsage[usage.ClientID] = make(map[string]clientUsageSnapshot)
		}
		current := s.clientUsage[usage.ClientID][agentID]
		current.ClientID = usage.ClientID
		if applyTrafficDelta {
			current.TrafficUsedBytes += usage.TrafficUsedBytes
		}
		current.UniqueIPsUsed = usage.UniqueIPsUsed
		current.ActiveTCPConns = usage.ActiveTCPConns
		current.ActiveUniqueIPs = usage.ActiveUniqueIPs
		current.ObservedAt = usage.ObservedAt
		s.clientUsage[usage.ClientID][agentID] = current
		toPersist = append(toPersist, storage.ClientUsageRecord{
			ClientID:         usage.ClientID,
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
// (when configured). Per-row failures are logged but never abort the loop.
func (s *Server) persistClientUsageRecords(ctx context.Context, toPersist []storage.ClientUsageRecord) {
	if s.store == nil {
		return
	}
	for _, rec := range toPersist {
		if err := s.store.UpsertClientUsage(ctx, rec); err != nil {
			s.logger.Warn("persist client_usage",
				"client_id", rec.ClientID, "agent_id", rec.AgentID, "error", err)
		}
	}
}

// zeroLiveGaugesForUntouchedClients zeros the live connection/IP gauges of
// any client that this agent did not include in the snapshot. Accumulated
// traffic is preserved — deleting the entry would lose the seeded/historical
// total.
func (s *Server) zeroLiveGaugesForUntouchedClients(agentID string, seen map[string]struct{}) {
	for clientID, usageByAgent := range s.clientUsage {
		current, ok := usageByAgent[agentID]
		if !ok {
			continue
		}
		if _, ok := seen[clientID]; ok {
			continue
		}
		current.ActiveTCPConns = 0
		current.ActiveUniqueIPs = 0
		usageByAgent[agentID] = current
	}
}

func (s *Server) applyClientIPSnapshot(agentID string, clients []clientIPSnapshot) {
	for _, snapshot := range clients {
		usageByAgent := s.clientUsage[snapshot.ClientID]
		if usageByAgent == nil {
			usageByAgent = make(map[string]clientUsageSnapshot)
			s.clientUsage[snapshot.ClientID] = usageByAgent
		}
		current := usageByAgent[agentID]
		current.ClientID = snapshot.ClientID
		current.UniqueIPsUsed = len(snapshot.ActiveIPs)
		current.ActiveUniqueIPs = len(snapshot.ActiveIPs)
		usageByAgent[agentID] = current
	}
}
