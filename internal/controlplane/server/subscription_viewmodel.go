package server

import (
	"context"
	"sort"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/subscription"
)

// subscriptionView is the full render model for one client's /sub page.
type subscriptionView struct {
	ClientName        string
	ExpirationRFC3339 string // "" => no expiry
	TrafficUsedBytes  int64
	DataQuotaBytes    int64 // 0 => unlimited
	Nodes             []subscriptionNode
}

type subscriptionNode struct {
	NodeName string
	Health   string // "online" | "degraded" | "offline"
	Links    []subscription.Link
}

// nodeInfo is the minimal per-agent context the grouping needs.
type nodeInfo struct {
	NodeName string
	Health   string
}

// groupDeploymentLinks builds the node list from a client's deployments.
// linksByAgent maps agentID -> raw connection links (caller passes only
// succeeded, non-empty deployments). nodes maps agentID -> display info.
// Agents whose links all fail to parse are dropped; nodes are sorted by
// NodeName for stable output.
func groupDeploymentLinks(linksByAgent map[string][]string, nodes map[string]nodeInfo) []subscriptionNode {
	out := make([]subscriptionNode, 0, len(linksByAgent))
	for agentID, raws := range linksByAgent {
		parsed := make([]subscription.Link, 0, len(raws))
		for _, raw := range raws {
			if link, ok := subscription.ParseLink(raw); ok {
				parsed = append(parsed, link)
			}
		}
		if len(parsed) == 0 {
			continue
		}
		info := nodes[agentID]
		name := info.NodeName
		if name == "" {
			name = agentID
		}
		health := info.Health
		if health == "" {
			health = "offline"
		}
		out = append(out, subscriptionNode{NodeName: name, Health: health, Links: parsed})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeName < out[j].NodeName })
	return out
}

// buildSubscriptionView loads a client's deployments, agents, presence, and
// usage and assembles the render model. Read-only.
func (s *Server) buildSubscriptionView(ctx context.Context, client clients.Client) (subscriptionView, error) {
	// 1. Load agents to build agentID -> nodeInfo map (NodeName + health).
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return subscriptionView{}, err
	}
	now := s.now()
	nodeMap := make(map[string]nodeInfo, len(agents))
	for _, a := range agents {
		health := string(s.presence.Evaluate(a.ID, now))
		nodeMap[a.ID] = nodeInfo{NodeName: a.NodeName, Health: health}
	}

	// 2. Load this client's deployments from the mirror (same source as the
	// client-detail page, so they stay consistent).
	mirror := s.clientsSvc.MirrorSnapshot()
	deploymentsByAgent := mirror.Deployments[client.ID]

	// 3. Collect connection links for succeeded deployments with non-empty links.
	linksByAgent := make(map[string][]string, len(deploymentsByAgent))
	for agentID, d := range deploymentsByAgent {
		if d.Status != clientDeploymentStatusSucceeded {
			continue
		}
		if len(d.ConnectionLinks) == 0 {
			continue
		}
		linksByAgent[agentID] = d.ConnectionLinks
	}

	// 4. Usage aggregate for TrafficUsedBytes.
	usageByAgent := s.clientsSvc.MirrorUsageByAgent(string(client.ID))
	agg := s.clientsSvc.AggregateUsage(usageByAgent)

	return subscriptionView{
		ClientName:        client.Name,
		ExpirationRFC3339: client.ExpirationRFC3339,
		TrafficUsedBytes:  int64(agg.TrafficUsedBytes), //nolint:gosec
		DataQuotaBytes:    client.DataQuotaBytes,
		Nodes:             groupDeploymentLinks(linksByAgent, nodeMap),
	}, nil
}
