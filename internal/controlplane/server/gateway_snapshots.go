package server

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// handleSnapshotMessage translates the wire-format snapshot into the internal
// agentSnapshot and enqueues it for the regular processor goroutine. Splitting
// this out keeps processRegularAgentMessage's CC under threshold by isolating
// the per-client and per-clientIP loops behind named helpers.
func (s *Server) handleSnapshotMessage(connectionCtx context.Context, agentID string, regularSnapshots chan agentSnapshot, snap *gatewayrpc.Snapshot) {
	s.logger.Debug(logMessageReceived, "agent_id", agentID, "type", "snapshot")
	observedAt := time.Unix(snap.ObservedAtUnix, 0).UTC()

	instances := convertInstanceSnapshots(snap.Instances)
	clients, usageResolved, usageSkipped := s.convertClientUsageSnapshots(agentID, snap.Clients, observedAt)
	if len(snap.Clients) > 0 {
		s.logger.Info("client usage snapshot received", "agent_id", agentID, "total", len(snap.Clients), "resolved", usageResolved, "skipped", usageSkipped)
	}
	clientIPs, ipResolved, ipSkipped := s.convertClientIPSnapshots(agentID, snap.ClientIps)
	if len(snap.ClientIps) > 0 {
		s.logger.Info("client ip snapshot received", "agent_id", agentID, "total", len(snap.ClientIps), "resolved", ipResolved, "skipped", ipSkipped)
	}

	enqueueRegularSnapshot(connectionCtx, regularSnapshots, agentSnapshot{
		AgentID:                  agentID,
		NodeName:                 snap.NodeName,
		FleetGroupID:             snap.FleetGroupId,
		Version:                  snap.Version,
		ReadOnly:                 snap.ReadOnly,
		Instances:                instances,
		Clients:                  clients,
		HasClients:               snap.HasClientUsage,
		ClientIPs:                clientIPs,
		HasClientIPs:             snap.HasClientIps,
		Runtime:                  snap.Runtime,
		HasRuntime:               snap.Runtime != nil,
		RuntimeDiagnostics:       snap.RuntimeDiagnostics,
		RuntimeSecurityInventory: snap.RuntimeSecurityInventory,
		Metrics:                  snap.Metrics,
		ObservedAt:               observedAt,
		Partial:                  snap.Partial,
	})
}

// convertInstanceSnapshots maps wire instances to the internal type.
func convertInstanceSnapshots(in []*gatewayrpc.InstanceSnapshot) []instanceSnapshot {
	instances := make([]instanceSnapshot, 0, len(in))
	for _, instance := range in {
		instances = append(instances, instanceSnapshot{
			ID:                instance.Id,
			Name:              instance.Name,
			Version:           instance.Version,
			ConfigFingerprint: instance.ConfigFingerprint,
			ManagedConfigHash: instance.GetManagedConfigHash(),
			ManagedConfigJSON: instance.GetManagedConfigJson(),
			Connections:       int(instance.Connections),
			ReadOnly:          instance.ReadOnly,
		})
	}
	return instances
}

// convertClientUsageSnapshots translates wire client usage rows, resolving
// missing client_ids by name. Returns the converted slice plus resolved/skipped
// counters for logging.
func (s *Server) convertClientUsageSnapshots(agentID string, in []*gatewayrpc.ClientUsageSnapshot, observedAt time.Time) ([]clientUsageSnapshot, int, int) {
	result := make([]clientUsageSnapshot, 0, len(in))
	var resolved, skipped int
	for _, client := range in {
		clientID := client.ClientId
		if clientID == "" && client.ClientName != "" {
			clientID = s.resolveClientIDByName(agentID, client.ClientName)
		}
		if clientID == "" {
			skipped++
			continue
		}
		resolved++
		result = append(result, clientUsageSnapshot{
			ClientID:           clients.ClientID(clientID),
			TrafficUsedBytes:   client.TrafficDeltaBytes,
			UniqueIPsUsed:      int(client.UniqueIpsUsed),
			ActiveTCPConns:     int(client.ActiveTcpConns),
			ActiveUniqueIPs:    int(client.ActiveUniqueIps),
			QuotaUsedBytes:     client.QuotaUsedBytes,
			QuotaLastResetUnix: client.QuotaLastResetUnix,
			ObservedAt:         observedAt,
			// P2-LOG-06 / L-07: carry the agent-side monotonic snapshot
			// sequence so the CP can dedup replays/restarts.
			Seq: client.Seq,
		})
	}
	return result, resolved, skipped
}

// convertClientIPSnapshots translates wire client-IP rows, resolving missing
// client_ids by name. Returns the converted slice plus resolved/skipped
// counters for logging.
func (s *Server) convertClientIPSnapshots(agentID string, in []*gatewayrpc.ClientIPSnapshot) ([]clientIPSnapshot, int, int) {
	clientIPs := make([]clientIPSnapshot, 0, len(in))
	var resolved, skipped int
	for _, clientIP := range in {
		ipClientID := clientIP.ClientId
		if ipClientID == "" && clientIP.ClientName != "" {
			ipClientID = s.resolveClientIDByName(agentID, clientIP.ClientName)
		}
		if ipClientID == "" {
			skipped++
			continue
		}
		resolved++
		clientIPs = append(clientIPs, clientIPSnapshot{
			ClientID:  ipClientID,
			ActiveIPs: append([]string(nil), clientIP.ActiveIps...),
		})
	}
	return clientIPs, resolved, skipped
}
