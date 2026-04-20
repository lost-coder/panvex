package main

import "testing"

func TestBootstrapEndpointURLRejectsNonLoopbackHTTPPanelURL(t *testing.T) {
	// Public hostname on plain http → rejected without explicit opt-in.
	_, err := bootstrapEndpointURL("http://panel.example.com", false)
	if err == nil {
		t.Fatal("bootstrapEndpointURL() error = nil, want insecure transport rejection")
	}
}

func TestBootstrapEndpointURLAcceptsPrivateNetworkHTTPWithoutFlag(t *testing.T) {
	// RFC1918 / CGNAT / ULA hosts are treated as private-network links
	// (typical VPN / LAN deployments). No `-insecure-transport` required
	// — the agent logs a warn instead of failing.
	cases := []string{
		"http://10.152.1.2:9443/ag",       // RFC1918 /8
		"http://172.16.0.5:9443/ag",       // RFC1918 /12
		"http://192.168.1.10:9443/ag",     // RFC1918 /16
		"http://100.64.0.5:9443/ag",       // CGNAT (Tailscale)
		"http://[fd00::1]:9443/ag",        // IPv6 ULA
		"http://169.254.10.1:9443/ag",     // IPv4 link-local
	}
	for _, panel := range cases {
		if _, err := bootstrapEndpointURL(panel, false); err != nil {
			t.Errorf("bootstrapEndpointURL(%q) error = %v, want nil", panel, err)
		}
	}
}

func TestBootstrapEndpointURLRejectsPublicIPHTTPWithoutFlag(t *testing.T) {
	// Public IP on plain http → rejected without explicit opt-in.
	_, err := bootstrapEndpointURL("http://8.8.8.8:9443/ag", false)
	if err == nil {
		t.Fatal("bootstrapEndpointURL(public ip) error = nil, want rejection")
	}
}

func TestBootstrapEndpointURLAllowsLoopbackHTTPPanelURL(t *testing.T) {
	endpoint, err := bootstrapEndpointURL("http://127.0.0.1:8080", false)
	if err != nil {
		t.Fatalf("bootstrapEndpointURL() error = %v", err)
	}
	if endpoint != "http://127.0.0.1:8080/api/agent/bootstrap" {
		t.Fatalf("bootstrapEndpointURL() = %q, want %q", endpoint, "http://127.0.0.1:8080/api/agent/bootstrap")
	}
}

func TestBootstrapEndpointURLAllowsInsecureTransportOptIn(t *testing.T) {
	endpoint, err := bootstrapEndpointURL("http://panel.internal:9443/ag", true)
	if err != nil {
		t.Fatalf("bootstrapEndpointURL(allowInsecure=true) error = %v", err)
	}
	want := "http://panel.internal:9443/ag/api/agent/bootstrap"
	if endpoint != want {
		t.Fatalf("bootstrapEndpointURL(allowInsecure=true) = %q, want %q", endpoint, want)
	}
}

func TestAgentRecoveryEndpointURLRejectsNonLoopbackHTTPPanelURL(t *testing.T) {
	_, err := agentRecoveryEndpointURL("http://panel.example.com", false)
	if err == nil {
		t.Fatal("agentRecoveryEndpointURL() error = nil, want insecure transport rejection")
	}
}

func TestAgentRecoveryEndpointURLAllowsInsecureTransportOptIn(t *testing.T) {
	endpoint, err := agentRecoveryEndpointURL("http://panel.internal:9443/ag", true)
	if err != nil {
		t.Fatalf("agentRecoveryEndpointURL(allowInsecure=true) error = %v", err)
	}
	want := "http://panel.internal:9443/ag/api/agent/recover-certificate"
	if endpoint != want {
		t.Fatalf("agentRecoveryEndpointURL(allowInsecure=true) = %q, want %q", endpoint, want)
	}
}
