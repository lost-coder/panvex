package clients

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// ReconcileDiscovered compares client data returned by an agent against
// the panel's managed clients and creates discovered_client records for unknown users.
func (s *Service) ReconcileDiscovered(ctx context.Context, agentID string, records []*gatewayrpc.ClientDetailRecord, telemtUnreachable bool, observedAt time.Time) {
	if telemtUnreachable {
		// The agent could not read Telemt's user list. An empty record set
		// here means "unknown", NOT "zero clients" — do not prune, do not
		// reconcile. The recovery-edge / periodic refresh will re-request a
		// real snapshot once Telemt is back.
		s.logger.WarnContext(ctx, "skipping discovery reconcile: agent reported telemt unreachable", "agent_id", agentID)
		return
	}
	if len(records) == 0 {
		return
	}

	managedNames, managedSecrets := s.managedClientIdentifiersForAgent(agentID)

	// seenNames is every user the node currently reports. HandleClientDataRequest
	// always returns the FULL set of configured Telemt users, so this is an
	// authoritative snapshot of what exists on the node right now (IN-M5).
	seenNames := make(map[string]struct{}, len(records))

	var disc, skippedManaged, skippedPanelID, orphaned int
	for _, record := range records {
		clientName := strings.TrimSpace(record.GetClientName())
		if clientName == "" {
			continue
		}
		seenNames[clientName] = struct{}{}

		// Skip clients that are already managed by the panel.
		if _, managed := managedNames[clientName]; managed {
			skippedManaged++
			continue
		}

		// Skip if the secret matches an already-managed client (same user, different name).
		secret := strings.TrimSpace(record.GetSecret())
		if secret != "" {
			if _, managed := managedSecrets[secret]; managed {
				// IN-L4: secret reuse under a DIFFERENT name on the node is an
				// operator anomaly (it masks a genuinely unmanaged user as
				// "managed"). The name is not in managedNames (checked above),
				// so log it as a conflict instead of silently swallowing it.
				s.logger.WarnContext(ctx, "discovered client shares a managed secret under a different name; skipping as managed",
					"agent_id", agentID,
					"client_name", clientName,
					"alert", "discovered_secret_name_conflict",
				)
				skippedManaged++
				continue
			}
		}

		// A record carrying a panel-assigned client_id is normally an echo of
		// our own rollout. But if the panel has no LIVING managed client
		// under that id (deleted, rotated away, or plain unknown), the node
		// is serving credentials the panel no longer vouches for — the
		// exact C1->C2 revocation-hole scenario from the rollout diagnosis.
		// Surface it instead of silently skipping it.
		if panelID := strings.TrimSpace(record.GetClientId()); panelID != "" {
			if s.clientIsLiveManaged(ctx, panelID) {
				skippedPanelID++
				continue
			}
			orphaned++
			s.recordDiscoveredOrphan(ctx, agentID, panelID, clientName, observedAt)
			continue
		}

		disc++
		s.upsertDiscoveredClient(ctx, agentID, record, observedAt)
	}
	s.logger.InfoContext(ctx, "reconciled discovered clients", "agent_id", agentID, "total", len(records), "new", disc, "managed", skippedManaged, "panel_assigned", skippedPanelID, "orphaned", orphaned)

	// IN-M5: prune pending discovered records for this agent that the node no
	// longer reports (e.g. the user was removed, or the agent's fleet group
	// changed and its managed clients were rolled off). Safe because we only
	// reach here on a non-empty response (early-return above) and the response
	// is the full user set. Only PENDING records are pruned — adopted/ignored
	// decisions are preserved.
	s.pruneStaleDiscoveredForAgent(ctx, agentID, seenNames)
}

// pruneStaleDiscoveredForAgent deletes pending discovered records owned by the
// agent whose client_name is absent from seenNames (the node's current full
// user set). Best-effort: list/delete failures are logged, not fatal.
func (s *Service) pruneStaleDiscoveredForAgent(ctx context.Context, agentID string, seenNames map[string]struct{}) {
	if s.discoveredRepo == nil {
		return
	}
	all, err := s.discoveredRepo.List(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "pruneStaleDiscoveredForAgent: list failed", "agent_id", agentID, "error", err)
		return
	}
	pruned := 0
	for _, dc := range all {
		if dc.AgentID != agentID || dc.Status != discovered.StatusPending {
			continue
		}
		if _, seen := seenNames[dc.ClientName]; seen {
			continue
		}
		if err := s.discoveredRepo.Delete(ctx, dc.ID); err != nil {
			s.logger.WarnContext(ctx, "pruneStaleDiscoveredForAgent: delete failed",
				"agent_id", agentID, "discovered_id", string(dc.ID), "error", err)
			continue
		}
		pruned++
	}
	if pruned > 0 {
		s.logger.InfoContext(ctx, "pruned stale discovered clients", "agent_id", agentID, "pruned", pruned)
	}
}

// managedClientIdentifiersForAgent returns the set of client names and secrets deployed on an agent.
func (s *Service) managedClientIdentifiersForAgent(agentID string) (names map[string]struct{}, secrets map[string]struct{}) {
	return s.MirrorIdentifiersForAgent(agentID)
}

// clientIsLiveManaged reports whether panelID resolves to a managed client
// the panel still vouches for (R10b Task 3 / C1->C2 revocation-hole guard).
//
// clients.Service.Get reads only the in-memory mirror. Verified: the mirror
// never holds a tombstoned Client. Runtime Service.Delete both soft-deletes
// the row in storage (deleted_at_unix) AND evicts the entry from the mirror
// outright via deleteMirrorClientLocked — it does not leave a Client with
// DeletedAt set in place. Startup Restore rebuilds the mirror exclusively
// from Repository.List, whose query filters `WHERE deleted_at_unix IS NULL`,
// so a tombstoned client never re-enters the mirror after a restart either.
// That means "not found" (err != nil) already covers every real tombstone
// path today. The DeletedAt check below is defense in depth in case that
// invariant ever changes (e.g. a future caller starts writing tombstoned
// values into the mirror directly via MirrorReplaceInMemory/SaveState,
// which do not special-case DeletedAt).
func (s *Service) clientIsLiveManaged(ctx context.Context, panelID string) bool {
	client, err := s.Get(ctx, ClientID(panelID))
	return err == nil && client.DeletedAt == nil
}

// recordDiscoveredOrphan surfaces a discovered record whose panel-assigned
// client_id does not resolve to a live managed client. Unlike
// upsertDiscoveredClient this deliberately does NOT create a discovered_client
// row: the record has a clear owner (the panel that issued the id), so
// "adopting" it would create a duplicate managed client. The operator's
// channel is this audit event + Warn log, plus the existing R10 reconciler,
// which re-sends the delete for ids the panel no longer owns and makes the
// node stop reporting it.
//
// Audited only on the first observation of a given (agentID, panelID) pair
// — reconcileDiscoveredClients runs on every telemetry tick, and without
// this dedup the same finding would be audited repeatedly. observedAt is
// accepted for signature symmetry with upsertDiscoveredClient's caller site
// and potential future use; the audit event's own timestamp comes from
// appendAuditWithContext (s.now()).
//
// CONCURRENCY: reconcileDiscoveredClients can run concurrently for
// different agents and does not hold s.mu itself. s.mu is taken ONLY for
// the check-and-set on discoveredOrphanSeen, then released before the
// audit write / log call — appendAuditWithContext must never run while
// s.mu is held (lock-ordering invariant, see clients_flow.go).
func (s *Service) recordDiscoveredOrphan(ctx context.Context, agentID, panelID, clientName string, observedAt time.Time) {
	_ = observedAt

	key := agentID + "|" + panelID
	s.mu.Lock()
	_, already := s.discoveredOrphanSeen[key]
	if !already {
		s.discoveredOrphanSeen[key] = struct{}{}
	}
	s.mu.Unlock()
	if already {
		return
	}

	s.logger.WarnContext(ctx, "discovered client carries a panel client_id with no live managed client backing it",
		"agent_id", agentID,
		"client_id", panelID,
		"client_name", clientName,
		"alert", "discovered_client_orphaned_panel_id",
	)
	s.deps.AppendAudit(ctx, "system", "clients.discovery_orphan", panelID, map[string]any{
		"agent_id":  agentID,
		"client_id": panelID,
		"name":      clientName,
	})
}

func (s *Service) upsertDiscoveredClient(ctx context.Context, agentID string, record *gatewayrpc.ClientDetailRecord, observedAt time.Time) {
	clientName := record.GetClientName()

	// P2-LOG-02 / L-10: before inserting a brand-new row, check whether a
	// discovered_clients row already exists for (agent_id, client_name).
	// If yes and it is still pending_review, update the existing row in
	// place — every agent reconnect triggers a FULL_SNAPSHOT, and without
	// this dedupe the pending-review list would grow unbounded. The
	// underlying UNIQUE (agent_id, client_name) constraint is a
	// belt-and-suspenders guard; this code path also avoids burning a new
	// sequence ID each time and keeps the audit log free of spurious
	// "clients.discovered" events for the same user.
	var (
		existing     discovered.DiscoveredClient
		haveExisting bool
		existingErr  error
	)
	if s.discoveredRepo != nil {
		existing, existingErr = s.discoveredRepo.GetByAgentAndName(ctx, agentID, clientName)
		switch {
		case existingErr == nil:
			haveExisting = true
		case errors.Is(existingErr, storage.ErrNotFound):
			// no-op: fall through to insert path
		default:
			s.logger.ErrorContext(ctx, "discovered client lookup failed", "client_name", clientName, "agent_id", agentID, "error", existingErr)
			return
		}
	}

	var id string
	if haveExisting {
		id = string(existing.ID)
	} else {
		id = s.NextDiscoveredID()
	}

	firstSeen := observedAt.UTC()
	if haveExisting {
		firstSeen = existing.FirstSeen
	}

	status := discovered.StatusPending
	if haveExisting {
		// Preserve non-pending status (ignored/adopted) across updates; only
		// refresh mutable observability fields. Without this guard a later
		// reconcile could resurrect an ignored row back to pending_review.
		if existing.Status != discovered.StatusPending {
			status = existing.Status
		}
	}

	dc := discovered.DiscoveredClient{
		ID:                 discovered.DiscoveredID(id),
		AgentID:            agentID,
		ClientName:         clientName,
		Secret:             record.GetSecret(),
		Status:             status,
		TotalOctets:        record.GetTotalOctets(),
		CurrentConnections: uint32(record.GetCurrentConnections()), //nolint:gosec
		ActiveUniqueIPs:    uint32(record.GetActiveUniqueIps()),    //nolint:gosec
		ConnectionLinks:    record.GetConnectionLinks(),
		MaxTCPConns:        int(record.GetMaxTcpConns()),      //nolint:gosec
		MaxUniqueIPs:       int(record.GetMaxUniqueIps()),     //nolint:gosec
		DataQuotaBytes:     int64(record.GetDataQuotaBytes()), //nolint:gosec
		Expiration:         record.GetExpiration(),
		FirstSeen:          firstSeen,
		UpdatedAt:          observedAt.UTC(),
	}

	if s.discoveredRepo != nil {
		if err := s.discoveredRepo.Save(ctx, dc); err != nil {
			s.logger.ErrorContext(ctx, "discovered client persistence failed", "client_name", dc.ClientName, "agent_id", agentID, "error", err)
			return
		}
	}

	// Only audit the first-time discovery; subsequent observations of the
	// same (agent, client) are just re-reports of the same finding.
	if !haveExisting {
		s.deps.AppendAudit(ctx, "system", "clients.discovered", id, map[string]any{
			"agent_id":    agentID,
			"client_name": dc.ClientName,
		})
	}
}
