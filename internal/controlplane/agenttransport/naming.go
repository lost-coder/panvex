package agenttransport

// agentServerNameSuffix is the fixed DNS zone (never resolved via real DNS)
// under which every agent cert carries a SAN. The panel sets
// tls.Config.ServerName to AgentServerName(agentID) when dialing a
// listen-mode agent, so standard x509 verification binds the connection to
// exactly that agent's certificate.
const agentServerNameSuffix = ".agents.panvex.internal"

// AgentServerName returns the DNS SAN / TLS ServerName for an agent.
func AgentServerName(agentID string) string {
	return agentID + agentServerNameSuffix
}
