package main

import "testing"

func TestBootstrapEndpointURLRejectsNonLoopbackHTTPPanelURL(t *testing.T) {
	_, err := bootstrapEndpointURL("http://panel.example.com", false)
	if err == nil {
		t.Fatal("bootstrapEndpointURL() error = nil, want insecure transport rejection")
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
