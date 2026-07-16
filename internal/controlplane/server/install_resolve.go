package server

import "net/http"

// ResolveInstallScriptURL returns the panel-hosted install-agent.sh URL
// for the current request, derived from the LIVE http.public_url panel
// setting (falling back to the request host). Honours
// PANVEX_INSTALL_SCRIPT_URL via installScriptPanelURL. Read per request so
// a saved http.public_url change takes effect without a restart.
//
// Exported so the provision-outbound handler renders the one-liner against
// the URL the panel is actually reachable on.
func (s *Server) ResolveInstallScriptURL(r *http.Request) string {
	base := buildAgentPublicURL(s.panelSettingsSnapshot(), s.panelRuntime, r.URL, s.trustedForwardedProto(r), r.Host)
	return installScriptPanelURL(base)
}

// ResolveAgentGRPCEndpoint returns the gRPC endpoint agents dial, derived
// from the LIVE grpc.public_endpoint panel setting (falling back to the
// request host). Read per request.
//
// Exported so the provision-outbound handler embeds the endpoint the agent
// will dial.
func (s *Server) ResolveAgentGRPCEndpoint(r *http.Request) string {
	endpoint, _ := s.bootstrapGatewayAddress(r.Host)
	return endpoint
}
