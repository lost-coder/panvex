package main

import "testing"

func TestBootstrapEndpointURLRejectsNonLoopbackHTTPPanelURL(t *testing.T) {
	_, err := bootstrapEndpointURL("http://panel.example.com")
	if err == nil {
		t.Fatal("bootstrapEndpointURL() error = nil, want insecure transport rejection")
	}
}

func TestBootstrapEndpointURLAllowsLoopbackHTTPPanelURL(t *testing.T) {
	endpoint, err := bootstrapEndpointURL("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("bootstrapEndpointURL() error = %v", err)
	}
	if endpoint != "http://127.0.0.1:8080/api/agent/bootstrap" {
		t.Fatalf("bootstrapEndpointURL() = %q, want %q", endpoint, "http://127.0.0.1:8080/api/agent/bootstrap")
	}
}

func TestAgentRecoveryEndpointURLRejectsNonLoopbackHTTPPanelURL(t *testing.T) {
	_, err := agentRecoveryEndpointURL("http://panel.example.com")
	if err == nil {
		t.Fatal("agentRecoveryEndpointURL() error = nil, want insecure transport rejection")
	}
}
