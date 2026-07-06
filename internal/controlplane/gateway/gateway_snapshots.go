package gateway

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// handleSnapshotMessage resolves client IDs on the wire snapshot and enqueues
// it for the regular processor goroutine. The wire message is carried as-is
// (AgentSnapshot is an envelope, not a projection); the proto→domain mapping
// happens once, in applyAgentSnapshot's helpers (P8.3, audit #23).
func (g *Gateway) handleSnapshotMessage(connectionCtx context.Context, agentID string, regularSnapshots chan AgentSnapshot, snap *gatewayrpc.Snapshot) {
	g.logger.DebugContext(connectionCtx, logMessageReceived, "agent_id", agentID, "type", "snapshot")
	observedAt := time.Unix(snap.ObservedAtUnix, 0).UTC()

	reports, usageResolved, usageSkipped := g.convertClientUsageSnapshots(agentID, snap.Clients, observedAt)
	if len(snap.Clients) > 0 {
		g.logger.InfoContext(connectionCtx, "client usage snapshot received", "agent_id", agentID, "total", len(snap.Clients), "resolved", usageResolved, "skipped", usageSkipped)
	}
	clientIPs, ipResolved, ipSkipped := g.convertClientIPSnapshots(agentID, snap.ClientIps)
	if len(snap.ClientIps) > 0 {
		g.logger.InfoContext(connectionCtx, "client ip snapshot received", "agent_id", agentID, "total", len(snap.ClientIps), "resolved", ipResolved, "skipped", ipSkipped)
	}

	enqueueRegularSnapshot(connectionCtx, regularSnapshots, AgentSnapshot{
		AgentID:    agentID,
		Snap:       snap,
		ObservedAt: observedAt,
		Clients:    reports,
		ClientIPs:  clientIPs,
	})
}

// convertClientUsageSnapshots translates wire client usage rows into
// inbound usage reports, resolving missing client_ids by name. Returns
// the converted slice plus resolved/skipped counters for logging.
func (g *Gateway) convertClientUsageSnapshots(agentID string, in []*gatewayrpc.ClientUsageSnapshot, observedAt time.Time) ([]clients.UsageReport, int, int) {
	result := make([]clients.UsageReport, 0, len(in))
	var resolved, skipped int
	for _, client := range in {
		clientID := client.ClientId
		if clientID == "" && client.ClientName != "" {
			clientID = g.deps.ResolveClientIDByName(agentID, client.ClientName)
		}
		if clientID == "" {
			skipped++
			continue
		}
		resolved++
		result = append(result, clients.UsageReport{
			ClientID: clients.ClientID(clientID),
			// P4: the agent-process-cumulative counter; the delta is
			// derived panel-side against the (boot_id, last_total)
			// watermark in mergeClientUsageBatch.
			TotalBytes:         client.TrafficTotalBytes,
			UniqueIPsUsed:      int(client.UniqueIpsUsed),
			ActiveTCPConns:     int(client.ActiveTcpConns),
			ActiveUniqueIPs:    int(client.ActiveUniqueIps),
			QuotaUsedBytes:     client.QuotaUsedBytes,
			QuotaLastResetUnix: client.QuotaLastResetUnix,
			ObservedAt:         observedAt,
		})
	}
	return result, resolved, skipped
}

// convertClientIPSnapshots translates wire client-IP rows, resolving missing
// client_ids by name. Returns the converted slice plus resolved/skipped
// counters for logging.
func (g *Gateway) convertClientIPSnapshots(agentID string, in []*gatewayrpc.ClientIPSnapshot) ([]ClientIPSnapshot, int, int) {
	clientIPs := make([]ClientIPSnapshot, 0, len(in))
	var resolved, skipped int
	for _, clientIP := range in {
		ipClientID := clientIP.ClientId
		if ipClientID == "" && clientIP.ClientName != "" {
			ipClientID = g.deps.ResolveClientIDByName(agentID, clientIP.ClientName)
		}
		if ipClientID == "" {
			skipped++
			continue
		}
		resolved++
		clientIPs = append(clientIPs, ClientIPSnapshot{
			ClientID:  ipClientID,
			ActiveIPs: append([]string(nil), clientIP.ActiveIps...),
		})
	}
	return clientIPs, resolved, skipped
}
