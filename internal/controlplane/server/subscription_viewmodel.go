package server

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/subscription"
)

//go:embed templates/subscription.html.tmpl
var subscriptionTemplateSource string

var subscriptionTemplate = template.Must(
	template.New("subscription").
		Funcs(template.FuncMap{
			// safeURL marks a string as a trusted URL so html/template does not
			// sanitise non-standard schemes such as tg://. Only explicitly
			// allowlisted schemes are passed through; anything else (e.g.
			// javascript:) collapses to "#" so a compromised agent cannot
			// inject an XSS payload via a connection link.
			"safeURL": func(s string) template.URL {
				if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "tg://") || strings.HasPrefix(s, "http://") {
					return template.URL(s) //nolint:gosec // scheme allowlisted above
				}
				return template.URL("#")
			},
		}).
		Parse(subscriptionTemplateSource),
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

// ---------- display helpers (called by subscriptionTemplate) ----------

func (v subscriptionView) HasQuota() bool { return v.DataQuotaBytes > 0 }

func (v subscriptionView) UsedPercent() int {
	if v.DataQuotaBytes <= 0 {
		return 0
	}
	p := float64(v.TrafficUsedBytes) / float64(v.DataQuotaBytes) * 100
	if p > 100 {
		p = 100
	}
	return int(p)
}

func (v subscriptionView) TrafficHuman() string {
	if v.DataQuotaBytes > 0 {
		return fmt.Sprintf("%s / %s", humanBytes(v.TrafficUsedBytes), humanBytes(v.DataQuotaBytes))
	}
	return fmt.Sprintf("%s · без лимита", humanBytes(v.TrafficUsedBytes))
}

func (v subscriptionView) ExpirationHuman() string {
	t, err := time.Parse(time.RFC3339, v.ExpirationRFC3339)
	if err != nil {
		return v.ExpirationRFC3339
	}
	return t.Format("02.01.2006")
}

func (n subscriptionNode) HealthLabel() string {
	switch n.Health {
	case "online":
		return "работает"
	case "degraded":
		return "возможны сбои"
	default:
		return "недоступен"
	}
}

// humanBytes formats b as a human-readable byte count with Cyrillic unit
// suffixes (Б, КБ, МБ, ГБ, ТБ). The unit strings are stored as a []string so
// that indexing selects a whole suffix rather than a raw UTF-8 byte.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d Б", b)
	}
	units := []string{"КБ", "МБ", "ГБ", "ТБ"}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < len(units)-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), units[exp])
}
