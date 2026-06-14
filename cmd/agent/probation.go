package main

import (
	"log/slog"
	"time"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
)

const defaultTransportProbation = 10 * time.Minute

// maybeRevertTransportSwitch rolls the transport state back to the
// pre-switch snapshot when no panel session has been established within the
// probation window (A2). Mutates *creds and persists to stateFile. Returns
// true when a revert was performed so the caller refreshes its derived
// gateway address/server-name.
func maybeRevertTransportSwitch(stateFile string, creds *agentstate.Credentials, window time.Duration, now time.Time) bool {
	if creds.PrevTransport == nil || creds.TransportSwitchedAtUnix == 0 {
		return false
	}
	if window <= 0 {
		window = defaultTransportProbation
	}
	switchedAt := time.Unix(creds.TransportSwitchedAtUnix, 0)
	if now.Sub(switchedAt) < window {
		return false
	}
	prev := creds.PrevTransport
	slog.Warn("transport probation expired without a panel session; reverting transport mode",
		"from", creds.TransportMode, "to", prev.Mode,
		"switched_at", switchedAt.UTC().Format(time.RFC3339))
	creds.TransportMode = prev.Mode
	creds.ListenAddr = prev.ListenAddr
	if prev.GRPCEndpoint != "" {
		creds.GRPCEndpoint = prev.GRPCEndpoint
	}
	if prev.GRPCServerName != "" {
		creds.GRPCServerName = prev.GRPCServerName
	}
	creds.PrevTransport = nil
	creds.TransportSwitchedAtUnix = 0
	if err := agentstate.Save(stateFile, *creds); err != nil {
		slog.Error("transport probation: persist revert failed", "error", err)
	}
	return true
}

// clearTransportProbation confirms the post-switch transport: called when a
// panel session is established. No-op when probation is not active.
func clearTransportProbation(stateFile string, creds *agentstate.Credentials) {
	if creds.PrevTransport == nil && creds.TransportSwitchedAtUnix == 0 {
		return
	}
	creds.PrevTransport = nil
	creds.TransportSwitchedAtUnix = 0
	if err := agentstate.Save(stateFile, *creds); err != nil {
		slog.Warn("transport probation: persist clear failed", "error", err)
		return
	}
	slog.Info("transport probation cleared: panel session established")
}
