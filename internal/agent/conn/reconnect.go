package conn

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/creds"
	"github.com/lost-coder/panvex/internal/agent/jobs"
	"github.com/lost-coder/panvex/internal/agent/probation"
	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
)

const (
	// gatewayStreamConnectTimeout bounds the dial / accept step of one
	// connection attempt.
	gatewayStreamConnectTimeout = 15 * time.Second
	// runtimeInitializationFastInterval is the poll interval used while Telemt
	// is still initialising, before it settles into steady state.
	runtimeInitializationFastInterval = 3 * time.Second
)

// ErrCredentialsRefreshed is returned by a connection whose certificate was
// renewed mid-stream: the credentials on disk changed, so the connection is
// torn down and the loop reconnects with the new identity. It is not a failure
// and does not consume a backoff attempt.
var ErrCredentialsRefreshed = errors.New("runtime credentials refreshed")

// Loop is the agent's outer main-loop: refresh certs, run the gRPC stream, and
// reconnect with backoff on failure. It owns the state that must survive an
// individual connection — the derived dial target, the credentials bundle, the
// in-flight job tracker (B4), the runtime-event ring, and the one-shot
// self-update backup cleanup. Construct it in the agent entrypoint and call Run.
type Loop struct {
	// StateFile is the on-disk credential bundle.
	StateFile string
	// GatewayAddr / ServerName are the derived dial target. They are refreshed
	// from disk whenever a transport reload or a probation revert changes them,
	// which is why the Loop owns them rather than the caller's flag struct.
	GatewayAddr string
	ServerName  string
	// Credentials is the live credentials bundle, replaced after every renewal,
	// recovery, reload and connection.
	Credentials agentstate.Credentials

	Agent                 *runtime.Agent
	Schedule              Schedule
	Reload                *TransportReload
	Reporter              *EnrollmentReporter
	Events                RuntimeEvents
	ClientDataConcurrency int
	TransportProbation    time.Duration

	// backupCleanup gates the one-shot removal of the self-update backup
	// binary across the whole process (was cmd/agent's updateBackupCleanupOnce).
	backupCleanup sync.Once
}

// Run drives the connect / reconnect cycle until ctx is cancelled or the panel
// deregisters this agent.
//
// Returns agentrevocation.ErrAgentRevoked when the panel signals (via the
// AGENT_REVOKED ErrorInfo on a PermissionDenied status) that this agent
// has been deregistered. In that case there is no point reconnecting —
// the propagated sentinel maps to exit code 78 in main, paired with
// systemd's RestartPreventExitStatus=78 in the unit file written by
// install-agent.sh. Any other error keeps the historic forever-retry
// behaviour.
func (l *Loop) Run(supervisorCtx context.Context) error {
	reconnectAttempt := 0
	// B4: the in-flight tracker outlives individual connections so a job
	// re-delivered right after a reconnect cannot run concurrently with its
	// still-draining first execution from the previous connection.
	jobInflight := jobs.NewInflightTracker()
	for {
		// Honour shutdown before we begin another iteration.
		if err := supervisorCtx.Err(); err != nil {
			return err
		}

		// If a switch_transport_mode job fired during the last connection, reload
		// state from disk so the new mode and listen address take effect.
		if l.Reload.takePending() {
			if reloaded, loadErr := agentstate.Load(l.StateFile); loadErr == nil {
				l.Credentials = reloaded
				if reloaded.GRPCEndpoint != "" {
					l.GatewayAddr = reloaded.GRPCEndpoint
				}
				if reloaded.GRPCServerName != "" {
					l.ServerName = reloaded.GRPCServerName
				}
			} else {
				slog.Error("transport reload: failed to reload state from disk", "error", loadErr)
			}
		}

		// A2: probation expiry check. On revert, refresh the derived dial
		// target the same way the transport-reload path above does.
		if probation.MaybeRevert(l.StateFile, &l.Credentials, l.TransportProbation, time.Now()) {
			if l.Credentials.GRPCEndpoint != "" {
				l.GatewayAddr = l.Credentials.GRPCEndpoint
			}
			if l.Credentials.GRPCServerName != "" {
				l.ServerName = l.Credentials.GRPCServerName
			}
		}

		// In listen mode the agent has no outbound dial route, so handle two
		// distinct cert lifecycle windows differently:
		//   - BEFORE expiry: in-stream renewal (RenewalRequest/RenewalResponse
		//     over the Connect bidi-stream) closes the window; the dial-mode
		//     unary pre-connection RenewCertificate RPC is skipped.
		//   - AFTER expiry: the cert is dead — no mTLS handshake can complete
		//     (neither the panel's dial-in nor in-stream renewal). Use the HTTP
		//     recovery flow (creds.RecoverListenIfExpired) which works over
		//     plain HTTPS to PanelURL regardless of transport mode.
		if l.Credentials.TransportMode == "listen" {
			refreshCtx, cancelRefresh := context.WithTimeout(supervisorCtx, creds.RefreshTimeout)
			recovered, recErr := creds.RecoverListenIfExpired(refreshCtx, l.StateFile, l.Credentials, nil, time.Now())
			cancelRefresh()
			if recErr != nil {
				if supervisorCtx.Err() != nil {
					return supervisorCtx.Err()
				}
				reconnectAttempt++
				slog.Error("listen mode: certificate recovery failed; will retry",
					"error", recErr,
					"hint", "create a recovery grant: POST /api/agents/{id}/certificate-recovery-grants")
				if waitErr := waitWithCancel(supervisorCtx, reconnectDelay(reconnectAttempt)); waitErr != nil {
					return waitErr
				}
				continue
			}
			l.Credentials = recovered
		}

		// In dial mode: use the pre-connection unary RenewCertificate RPC for
		// cert refresh. This path is skipped in listen mode (handled above).
		if l.Credentials.TransportMode != "listen" {
			// Derive the refresh ctx from supervisorCtx so SIGTERM during
			// the renewal RPC also unblocks promptly.
			refreshCtx, cancelRefresh := context.WithTimeout(supervisorCtx, creds.RefreshTimeout)
			refreshed, err := creds.RenewIfNeeded(refreshCtx, l.StateFile, l.GatewayAddr, l.ServerName, l.Credentials, time.Now())
			cancelRefresh()
			if err != nil {
				if agentrevocation.IsAgentRevoked(err) {
					return agentrevocation.ErrAgentRevoked
				}
				if supervisorCtx.Err() != nil {
					return supervisorCtx.Err()
				}
				reconnectAttempt++
				// A wrapped context.Canceled at this point means the refresh
				// RPC was torn down by a near-simultaneous shutdown — the
				// outer ctx.Err() check above just missed it. Demote to Info
				// so shutdown does not produce a stray Error line.
				if errors.Is(err, context.Canceled) {
					slog.Info("certificate refresh aborted (context cancelled)")
				} else {
					slog.Error("certificate refresh failed", "error", err)
				}
				if waitErr := waitWithCancel(supervisorCtx, reconnectDelay(reconnectAttempt)); waitErr != nil {
					return waitErr
				}
				continue
			}
			l.Credentials = refreshed
		}

		afterConn, connErr := runConnection(supervisorCtx, runConnectionParams{
			gatewayAddr:           l.GatewayAddr,
			serverName:            l.ServerName,
			stateFile:             l.StateFile,
			credentialsState:      l.Credentials,
			agent:                 l.Agent,
			schedule:              l.Schedule,
			clientDataConcurrency: l.ClientDataConcurrency,
			tr:                    l.Reload,
			reporter:              l.Reporter,
			jobInflight:           jobInflight,
			transportProbation:    l.TransportProbation,
			events:                l.Events,
			backupCleanup:         &l.backupCleanup,
		})
		l.Credentials = afterConn
		if connErr == nil || errors.Is(connErr, ErrCredentialsRefreshed) {
			reconnectAttempt = 0
			continue
		}
		if agentrevocation.IsAgentRevoked(connErr) {
			return agentrevocation.ErrAgentRevoked
		}
		if supervisorCtx.Err() != nil {
			return supervisorCtx.Err()
		}
		reconnectAttempt++
		// A connection that ends because its own connection ctx was cancelled
		// (e.g. switch_transport_mode job dropping the stream, in-stream cert
		// renewal triggering a reconnect, or a server-side close that races a
		// shutdown signal) surfaces a wrapped context.Canceled here. That is
		// not an Error condition — it's expected lifecycle. Demote to Info so
		// the reconnect remains visible without polluting the Error stream.
		if errors.Is(connErr, context.Canceled) {
			slog.Info("connection ended (context cancelled); reconnecting")
		} else {
			slog.Error("connection ended", "error", connErr)
		}
		if waitErr := waitWithCancel(supervisorCtx, reconnectDelay(reconnectAttempt)); waitErr != nil {
			return waitErr
		}
	}
}

// waitWithCancel sleeps for d, returning early with ctx.Err() if the
// supervisor ctx is cancelled. Replaces bare time.Sleep so a SIGTERM
// during the reconnect backoff (up to 45s) does not hold up shutdown.
func waitWithCancel(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// reconnectBaseDelay / reconnectMaxDelay bound the exponential backoff of the
// agent's outer reconnect loop. The cap stays well below the panel's 90s
// offline threshold (presence tracker defaults, lifecycle.go), so even an
// agent sleeping a full max window keeps its presence within "degraded".
const (
	reconnectBaseDelay = time.Second
	reconnectMaxDelay  = 45 * time.Second
)

// reconnectDelay returns the backoff for the given attempt with full jitter:
// the deterministic exponential value is the ceiling and the actual sleep is
// uniform in [ceiling/2, ceiling]. Mirrors agenttransport/outbound.go's
// jitter() so both transport directions damp herd reconnects identically.
// Without jitter a panel restart made the whole fleet retry in lockstep
// waves (D1). The attempt counter is reset by the caller on any successful
// connection (Loop.Run).
func reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := reconnectBaseDelay << min(attempt-1, 6)
	if delay > reconnectMaxDelay {
		delay = reconnectMaxDelay
	}
	return delay/2 + time.Duration(rand.Int64N(int64(delay/2+1)))
}
