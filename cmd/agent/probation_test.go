package main

import (
	"path/filepath"
	"testing"
	"time"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
)

func writeProbationState(t *testing.T, creds agentstate.Credentials) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := agentstate.Save(path, creds); err != nil {
		t.Fatalf("save: %v", err)
	}
	return path
}

func TestMaybeRevertTransportSwitchRestoresPreviousState(t *testing.T) {
	switchedAt := time.Now().Add(-15 * time.Minute)
	creds := agentstate.Credentials{
		AgentID:       "a1",
		TransportMode: "listen",
		ListenAddr:    ":8443",
		GRPCEndpoint:  "panel:8443",
		PrevTransport: &agentstate.TransportSnapshot{
			Mode: "dial", GRPCEndpoint: "panel:8443", GRPCServerName: "control-plane.panvex.internal",
		},
		TransportSwitchedAtUnix: switchedAt.Unix(),
	}
	path := writeProbationState(t, creds)

	if !maybeRevertTransportSwitch(path, &creds, 10*time.Minute, time.Now()) {
		t.Fatal("expected revert after probation window elapsed")
	}
	if creds.TransportMode != "dial" || creds.PrevTransport != nil || creds.TransportSwitchedAtUnix != 0 {
		t.Fatalf("in-memory state not reverted: %+v", creds)
	}
	onDisk, err := agentstate.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if onDisk.TransportMode != "dial" || onDisk.PrevTransport != nil {
		t.Fatalf("on-disk state not reverted: %+v", onDisk)
	}
}

func TestMaybeRevertTransportSwitchWaitsOutTheWindow(t *testing.T) {
	creds := agentstate.Credentials{
		AgentID:                 "a1",
		TransportMode:           "listen",
		PrevTransport:           &agentstate.TransportSnapshot{Mode: "dial"},
		TransportSwitchedAtUnix: time.Now().Add(-1 * time.Minute).Unix(),
	}
	path := writeProbationState(t, creds)
	if maybeRevertTransportSwitch(path, &creds, 10*time.Minute, time.Now()) {
		t.Fatal("must not revert inside the probation window")
	}
	if creds.TransportMode != "listen" {
		t.Fatal("state mutated without revert")
	}
}

func TestClearTransportProbationPersists(t *testing.T) {
	creds := agentstate.Credentials{
		AgentID:                 "a1",
		TransportMode:           "listen",
		PrevTransport:           &agentstate.TransportSnapshot{Mode: "dial"},
		TransportSwitchedAtUnix: time.Now().Unix(),
	}
	path := writeProbationState(t, creds)
	clearTransportProbation(path, &creds)
	if creds.PrevTransport != nil || creds.TransportSwitchedAtUnix != 0 {
		t.Fatalf("in-memory probation not cleared: %+v", creds)
	}
	onDisk, _ := agentstate.Load(path)
	if onDisk.PrevTransport != nil || onDisk.TransportSwitchedAtUnix != 0 {
		t.Fatalf("on-disk probation not cleared: %+v", onDisk)
	}
}
