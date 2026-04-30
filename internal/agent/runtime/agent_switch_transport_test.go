package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestHandleSwitchTransportModeJobPersistsState(t *testing.T) {
	var captured struct {
		mode, listenAddr, panelURL string
	}
	a := New(Config{
		AgentID: "agent-1",
		UpdateTransport: func(mode, listenAddr, panelURL string) error {
			captured.mode = mode
			captured.listenAddr = listenAddr
			captured.panelURL = panelURL
			return nil
		},
	}, &fakeTelemtClient{})

	payload, _ := json.Marshal(map[string]string{
		"mode":        "listen",
		"listen_addr": ":8443",
	})
	job := &gatewayrpc.JobCommand{
		Id:       "j1",
		Action:      "switch_transport_mode",
		PayloadJson: string(payload),
	}
	result := a.HandleJob(context.Background(), job, time.Now())

	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Message)
	}
	if captured.mode != "listen" || captured.listenAddr != ":8443" {
		t.Fatalf("UpdateTransport not invoked correctly: %+v", captured)
	}
}

func TestHandleSwitchTransportModeJobDialMode(t *testing.T) {
	var captured struct {
		mode, listenAddr, panelURL string
	}
	a := New(Config{
		AgentID: "agent-1",
		UpdateTransport: func(mode, listenAddr, panelURL string) error {
			captured.mode = mode
			captured.listenAddr = listenAddr
			captured.panelURL = panelURL
			return nil
		},
	}, &fakeTelemtClient{})

	payload, _ := json.Marshal(map[string]string{
		"mode":      "dial",
		"panel_url": "https://panel.example.com",
	})
	job := &gatewayrpc.JobCommand{
		Id:       "j2",
		Action:      "switch_transport_mode",
		PayloadJson: string(payload),
	}
	result := a.HandleJob(context.Background(), job, time.Now())

	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Message)
	}
	if captured.mode != "dial" || captured.panelURL != "https://panel.example.com" {
		t.Fatalf("UpdateTransport not invoked correctly: %+v", captured)
	}
}

func TestHandleSwitchTransportModeJobRejectsInvalidMode(t *testing.T) {
	a := New(Config{
		AgentID:         "agent-1",
		UpdateTransport: func(string, string, string) error { return nil },
	}, &fakeTelemtClient{})
	payload, _ := json.Marshal(map[string]string{"mode": "garbage"})
	result := a.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Action:      "switch_transport_mode",
		PayloadJson: string(payload),
	}, time.Now())
	if result.Success {
		t.Fatal("expected failure for invalid mode")
	}
}

func TestHandleSwitchTransportModeJobRejectsBadJSON(t *testing.T) {
	a := New(Config{
		AgentID:         "agent-1",
		UpdateTransport: func(string, string, string) error { return nil },
	}, &fakeTelemtClient{})
	result := a.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Action:      "switch_transport_mode",
		PayloadJson: "not-json",
	}, time.Now())
	if result.Success {
		t.Fatal("expected failure for invalid JSON payload")
	}
}

func TestHandleSwitchTransportModeJobToleratesNilCallback(t *testing.T) {
	a := New(Config{AgentID: "agent-1"}, &fakeTelemtClient{}) // no UpdateTransport
	payload, _ := json.Marshal(map[string]string{"mode": "dial"})
	result := a.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Action:      "switch_transport_mode",
		PayloadJson: string(payload),
	}, time.Now())
	if !result.Success {
		t.Fatalf("expected success ack with nil callback, got: %s", result.Message)
	}
}
