package agenttransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// errOutboundTLSMissing is returned by connectAndServe when no TLS config has
// been wired. Without it the panel would silently dial without mTLS, accepting
// any cert signed by a system CA — a security regression we surface loudly.
var errOutboundTLSMissing = errors.New("agenttransport: outbound TLS config is required but not set")

// EnrollFunc is called by outboundSupervisor.connectAndServe when the agent's
// bootstrap_state is "pending". It must complete the enrollment exchange before
// the normal mTLS dial proceeds. On success the DB transitions to "active" so
// the next iteration skips enrollment and goes straight to connectAndServe.
// On error the supervisor backs off and retries the whole cycle.
//
// The function receives the agent address and agent ID. It is responsible for
// choosing the appropriate (non-mTLS) TLS config for the enrollment dial.
// A nil EnrollFunc disables enrollment pre-flight entirely.
type EnrollFunc func(ctx context.Context, agentAddr, agentID string) error

// BootstrapStateFunc queries the current bootstrap_state for the given agent.
// Returns "pending", "active", "expired", or an error. A nil value causes
// the supervisor to skip the enrollment pre-flight and proceed directly to
// the mTLS dial (safe default for already-enrolled agents).
type BootstrapStateFunc func(ctx context.Context, agentID string) (string, error)

// Default outbound backoff constants — used as documentation defaults and
// as fallback values when no OperationalStore getter is wired.
const (
	outboundBackoffInitial = 1 * time.Second
	outboundBackoffMax     = 60 * time.Second
)

// outboundSupervisor maintains a single agent's outbound (panel-dials-agent)
// gRPC connection with exponential backoff + jitter on disconnects.
type outboundSupervisor struct {
	meta    NodeMeta
	tlsCfg  *tls.Config
	handler SessionHandler
	logger  *slog.Logger
	// backoffInitialFn and backoffMaxFn are called on each reconnect
	// iteration so that an operator change to agents.outbound_backoff_initial
	// / agents.outbound_backoff_max is picked up without restarting the
	// panel. When nil, the constants above are used.
	backoffInitialFn func() time.Duration
	backoffMaxFn     func() time.Duration

	// enrollFn, when non-nil, is called before the normal mTLS dial whenever
	// bootstrapStateFn reports "pending". See EnrollFunc for the contract.
	enrollFn EnrollFunc
	// bootstrapStateFn, when non-nil, is consulted at the top of each
	// connectAndServe iteration to decide whether enrollment is needed.
	bootstrapStateFn BootstrapStateFunc

	// pinReader, when non-nil, is consulted during the TLS handshake to
	// verify the agent's leaf certificate SPKI hash against the stored pin
	// (S-02). Nil disables pin verification (legacy agents pre-S-02).
	pinReader CertPinReader
	// pinObserver, when non-nil, is called after each pin verification with
	// result "ok", "mismatch", or "missing". Used for Prometheus metrics.
	pinObserver CertPinVerifyObserver

	// rec, when non-nil, records a per-dial enrollment timeline for the
	// outbound (panel-dials-agent) flow. Every call site is gated on
	// rec != nil && attemptID != "" so a missing recorder degrades to
	// existing behaviour without panicking. Persistence failures inside the
	// Recorder are logged and silently dropped — observability must never
	// abort an outbound reconnect.
	rec *enrollment.Recorder
}

func (s *outboundSupervisor) effectiveBackoffInitial() time.Duration {
	if s.backoffInitialFn != nil {
		return s.backoffInitialFn()
	}
	return outboundBackoffInitial
}

func (s *outboundSupervisor) effectiveBackoffMax() time.Duration {
	if s.backoffMaxFn != nil {
		return s.backoffMaxFn()
	}
	return outboundBackoffMax
}

func newOutboundSupervisor(meta NodeMeta, tlsCfg *tls.Config, h SessionHandler, l *slog.Logger) *outboundSupervisor {
	return &outboundSupervisor{
		meta:    meta,
		tlsCfg:  tlsCfg,
		handler: h,
		logger:  l,
	}
}

func (s *outboundSupervisor) run(ctx context.Context) {
	backoff := s.effectiveBackoffInitial()
	for {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		if err := s.connectAndServe(ctx); err != nil {
			s.logger.Warn("agenttransport: outbound session ended",
				"node_id", s.meta.NodeID, "addr", s.meta.DialAddress, "error", err)
		}
		// Reset backoff if the session lived long enough that any prior
		// failure-driven inflation should be considered ancient history.
		// Without this, a stable connection that occasionally flaps would
		// accumulate ever-longer reconnect delays.
		// Re-read max each iteration so an operator change takes effect
		// without restarting the panel.
		backoffMax := s.effectiveBackoffMax()
		if time.Since(start) >= backoffMax {
			backoff = s.effectiveBackoffInitial()
		}
		delay := jitter(backoff)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < backoffMax {
			backoff *= 2
			if backoff > backoffMax {
				backoff = backoffMax
			}
		}
	}
}

// connectAndServe runs one enrollment+connect cycle for the agent:
//  1. If bootstrapStateFn is set and reports "pending", enrollFn is invoked to
//     complete the certificate enrollment exchange. On error the function
//     returns so the supervisor's backoff loop retries.
//  2. After successful enrollment (or when state is already "active"), the
//     normal mTLS gRPC dial proceeds.
//
// Recorder calls are layered on top of the existing control flow: each dial
// cycle opens a fresh enrollment.Recorder attempt (mode=outbound) at the
// top, records timeline steps as the dial progresses, and marks the attempt
// failed/completed before returning. A nil Recorder or a Begin error skips
// recording — the underlying business logic is unchanged.
func (s *outboundSupervisor) connectAndServe(ctx context.Context) error {
	// Open a per-cycle attempt so the operator can see why a particular
	// dial succeeded or failed. attemptID="" disables every subsequent
	// recorder call (defence against Begin errors or missing recorder).
	var attemptID string
	if s.rec != nil {
		id, err := s.rec.Begin(ctx, enrollment.ModeOutbound, "", s.meta.DialAddress)
		if err != nil {
			s.logger.Warn("agenttransport: enrollment Begin failed",
				"node_id", s.meta.NodeID, "error", err)
		} else {
			attemptID = id
			// Tie the attempt to its agent row from the start so the UI
			// can surface it under the right agent without waiting for a
			// later AttachAgent.
			if attachErr := s.rec.AttachAgent(ctx, attemptID, s.meta.AgentID); attachErr != nil {
				s.logger.Warn("agenttransport: enrollment AttachAgent failed",
					"node_id", s.meta.NodeID, "attempt_id", attemptID, "error", attachErr)
			}
		}
	}
	// Best-effort cleanup: if this cycle returns without reaching the
	// Complete/Fail path explicitly (e.g. an early return after a future
	// refactor), mark the attempt failed so it doesn't dangle as
	// in_progress. No-op on already-terminal attempts.
	completed := false
	defer func() {
		if s.rec != nil && attemptID != "" && !completed {
			_ = s.rec.Fail(ctx, attemptID, enrollment.ErrInternal, nil, nil)
		}
	}()

	if s.rec != nil && attemptID != "" {
		s.rec.Event(ctx, attemptID, enrollment.StepPanelDialAttempted, enrollment.LevelInfo,
			"dialing agent", map[string]any{"addr": s.meta.DialAddress})
	}

	// Enrollment pre-flight: run if bootstrap_state is "pending".
	if s.bootstrapStateFn != nil && s.enrollFn != nil {
		state, err := s.bootstrapStateFn(ctx, s.meta.AgentID)
		if err != nil {
			if s.rec != nil && attemptID != "" {
				_ = s.rec.Fail(ctx, attemptID, enrollment.ErrInternal, err,
					map[string]any{"stage": "bootstrap_state_lookup"}) // failures are best-effort; primary err is what matters
				completed = true
			}
			return fmt.Errorf("agenttransport: bootstrap state lookup (node_id=%s): %w", s.meta.NodeID, err)
		}
		if state == "pending" {
			s.logger.Info("agenttransport: bootstrap_state=pending; running enrollment",
				"node_id", s.meta.NodeID, "addr", s.meta.DialAddress)
			if err := s.enrollFn(ctx, s.meta.DialAddress, s.meta.AgentID); err != nil {
				if s.rec != nil && attemptID != "" {
					_ = s.rec.Fail(ctx, attemptID, classifyDialError(err), err,
						map[string]any{"stage": "enroll"}) // failures are best-effort; primary err is what matters
					completed = true
				}
				return fmt.Errorf("agenttransport: enrollment (node_id=%s): %w", s.meta.NodeID, err)
			}
			s.logger.Info("agenttransport: enrollment completed; proceeding to mTLS dial",
				"node_id", s.meta.NodeID)
		}
	}

	if s.tlsCfg == nil {
		if s.rec != nil && attemptID != "" {
			_ = s.rec.Fail(ctx, attemptID, enrollment.ErrInternal, errOutboundTLSMissing,
				map[string]any{"stage": "tls_config"}) // failures are best-effort; primary err is what matters
			completed = true
		}
		return fmt.Errorf("%w (node_id=%s)", errOutboundTLSMissing, s.meta.NodeID)
	}

	// Clone the TLS config so we can install a per-dial VerifyConnection hook
	// without mutating the shared base config. (S-02)
	tlsCfg := s.tlsCfg.Clone()
	if s.pinReader != nil {
		agentID := s.meta.AgentID
		pinReader := s.pinReader
		pinObserver := s.pinObserver
		prevVerify := tlsCfg.VerifyConnection
		tlsCfg.VerifyConnection = func(state tls.ConnectionState) error {
			// Chain any pre-existing hook first (e.g., mTLS client-cert
			// validation wired by the panel CA setup).
			if prevVerify != nil {
				if err := prevVerify(state); err != nil {
					return err
				}
			}
			pin, err := pinReader.GetAgentCertPin(ctx, agentID)
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				return fmt.Errorf("agenttransport: cert pin lookup (node_id=%s): %w", agentID, err)
			}
			if len(pin) == 0 {
				// No pin stored — agent enrolled before S-02 or pin not yet
				// captured. Skip verification for this dial.
				if pinObserver != nil {
					pinObserver("missing")
				}
				return nil
			}
			var leaf *x509.Certificate
			if len(state.PeerCertificates) > 0 {
				leaf = state.PeerCertificates[0]
			}
			if err := verifyCertPin(leaf, pin); err != nil {
				if pinObserver != nil {
					pinObserver("mismatch")
				}
				return fmt.Errorf("%w (node_id=%s)", err, agentID)
			}
			if pinObserver != nil {
				pinObserver("ok")
			}
			return nil
		}
	}

	conn, err := grpc.NewClient(s.meta.DialAddress,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		if s.rec != nil && attemptID != "" {
			_ = s.rec.Fail(ctx, attemptID, classifyDialError(err), err,
				map[string]any{"stage": "grpc_new_client", "addr": s.meta.DialAddress}) // failures are best-effort; primary err is what matters
			completed = true
		}
		return err
	}
	defer conn.Close()
	client := gatewayrpc.NewAgentGatewayClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		if s.rec != nil && attemptID != "" {
			_ = s.rec.Fail(ctx, attemptID, classifyDialError(err), err,
				map[string]any{"stage": "grpc_connect", "addr": s.meta.DialAddress}) // failures are best-effort; primary err is what matters
			completed = true
		}
		return err
	}

	// Stream opened — the TLS handshake completed successfully (grpc.NewClient
	// is lazy, so handshake errors surface here on the first RPC). Record the
	// happy path then hand off to the session handler.
	if s.rec != nil && attemptID != "" {
		s.rec.Event(ctx, attemptID, enrollment.StepTLSHandshakeOK, enrollment.LevelInfo, "panel reached", nil)
		s.rec.Event(ctx, attemptID, enrollment.StepFirstSyncOK, enrollment.LevelInfo, "first sync ok", nil)
		if err := s.rec.Complete(ctx, attemptID); err != nil {
			s.logger.Warn("agenttransport: enrollment Complete failed",
				"node_id", s.meta.NodeID, "attempt_id", attemptID, "error", err)
		}
		completed = true
	}

	sess := &ClientStreamSession{Stream: stream}
	return s.handler(ctx, sess, s.meta)
}

// jitter returns a duration in [d/2, d] — full jitter dampens herd-style
// reconnects when many agents disconnect simultaneously.
func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int64N(int64(d/2+1)))
}

type outboundSupervisorEntry struct {
	cancel context.CancelFunc
}

// SupervisorGaugeDelta is called by outboundTransport whenever a supervisor
// is added (+1) or removed (-1). The concrete wiring is
// (*metricsCollectors).AddOutboundSupervisor in package server; tests can
// supply a simple closure. A nil value is treated as a no-op.
type SupervisorGaugeDelta func(delta float64)

// outboundTransport is the supervisor pool for outbound (reverse-mode) agents.
// supervisors maps nodeID to a live supervisor entry; wg tracks the spawned
// goroutines so stopAll can drain them synchronously.
type outboundTransport struct {
	tlsCfg  *tls.Config
	handler SessionHandler
	logger  *slog.Logger
	// onSupervisorDelta is called with +1 / -1 whenever a supervisor entry
	// is added or removed. Nil when metrics are not wired.
	onSupervisorDelta SupervisorGaugeDelta

	// enrollFn and bootstrapStateFn are wired by Manager.SetEnrollDriver and
	// forwarded to every outboundSupervisor created by ensureSupervisor.
	enrollFn         EnrollFunc
	bootstrapStateFn BootstrapStateFunc

	// pinReader and pinObserver are wired by Manager.SetCertPinReader and
	// forwarded to every outboundSupervisor for post-handshake SPKI
	// verification. (S-02)
	pinReader   CertPinReader
	pinObserver CertPinVerifyObserver

	// backoffInitialFn / backoffMaxFn are forwarded to every supervisor so
	// each reconnect iteration reads the current value from the
	// OperationalStore. Nil → each supervisor falls back to the package
	// constants (outboundBackoffInitial / outboundBackoffMax).
	backoffInitialFn func() time.Duration
	backoffMaxFn     func() time.Duration

	// rec is forwarded to every outboundSupervisor so each dial cycle can
	// record its own enrollment timeline. Nil disables recording.
	rec *enrollment.Recorder

	mu sync.RWMutex
	// lifecycleCtx is the parent context for every supervisor goroutine.
	// Wired by Manager.Start via setLifecycleCtx; defaults to
	// context.Background() so unit tests that bypass Manager.Start still
	// produce safe (if non-cancellable) supervisor contexts. Cancelling
	// this context cascades into all supervisors as a defence-in-depth
	// complement to the explicit per-entry cancel.
	lifecycleCtx context.Context
	// stopped is set by stopAll and gates ensureSupervisor so that no new
	// supervisors can be registered mid-shutdown (e.g. a late
	// OnNodeChanged firing after Manager.Stop). Read/written under mu.
	stopped     bool
	supervisors map[string]*outboundSupervisorEntry
	wg          sync.WaitGroup
}

func newOutboundTransport(tlsCfg *tls.Config, handler SessionHandler, logger *slog.Logger) *outboundTransport {
	return &outboundTransport{
		tlsCfg:       tlsCfg,
		handler:      handler,
		logger:       logger,
		lifecycleCtx: context.Background(),
		supervisors:  map[string]*outboundSupervisorEntry{},
	}
}

// setLifecycleCtx wires the parent context used as the root for all
// supervisor goroutines. Call this once at startup (Manager.Start) before
// any supervisor is registered. Cancellation of the parent cascades to all
// supervisors as a defence-in-depth complement to the explicit per-entry
// cancel.
func (t *outboundTransport) setLifecycleCtx(ctx context.Context) {
	t.mu.Lock()
	t.lifecycleCtx = ctx
	t.mu.Unlock()
}

// ensureSupervisor accepts a ctx purely for contextcheck/log-trace
// propagation in the caller; the supervisor goroutine derives its own
// lifetime from t.lifecycleCtx (set at Manager.Start) so it can outlive
// any individual caller request. The caller's ctx is therefore
// intentionally unused for cancellation but is required so the call
// site can satisfy contextcheck.
//
//nolint:contextcheck // supervisor lifetime is bound to t.lifecycleCtx, not the caller's ctx.
func (t *outboundTransport) ensureSupervisor(_ context.Context, meta NodeMeta) {
	t.mu.Lock()
	if t.stopped {
		// stopAll has run; refuse to register new supervisors so that no
		// goroutine outlives the shutdown defer chain. Closes the narrow
		// race where OnNodeChanged could fire after Manager.Stop.
		t.mu.Unlock()
		return
	}
	if _, exists := t.supervisors[meta.NodeID]; exists {
		t.mu.Unlock()
		return
	}
	//nolint:gosec // G118: supervisor cancel is stored in supervisors[meta.NodeID].cancel and invoked by stopAll/removeSupervisor.
	ctx, cancel := context.WithCancel(t.lifecycleCtx)
	t.supervisors[meta.NodeID] = &outboundSupervisorEntry{cancel: cancel}
	t.wg.Add(1)
	fn := t.onSupervisorDelta
	enrollFn := t.enrollFn
	bootstrapStateFn := t.bootstrapStateFn
	pinReader := t.pinReader
	pinObserver := t.pinObserver
	backoffInitialFn := t.backoffInitialFn
	backoffMaxFn := t.backoffMaxFn
	rec := t.rec
	t.mu.Unlock()

	if fn != nil {
		fn(+1)
	}
	sup := newOutboundSupervisor(meta, t.tlsCfg, t.handler, t.logger)
	sup.enrollFn = enrollFn
	sup.bootstrapStateFn = bootstrapStateFn
	sup.pinReader = pinReader
	sup.pinObserver = pinObserver
	sup.backoffInitialFn = backoffInitialFn
	sup.backoffMaxFn = backoffMaxFn
	sup.rec = rec
	go func() {
		defer t.wg.Done()
		sup.run(ctx)
	}()
}

func (t *outboundTransport) removeSupervisor(nodeID string) {
	t.mu.Lock()
	entry, ok := t.supervisors[nodeID]
	if ok {
		delete(t.supervisors, nodeID)
	}
	fn := t.onSupervisorDelta
	t.mu.Unlock()
	if ok {
		entry.cancel()
		if fn != nil {
			fn(-1)
		}
	}
}

func (t *outboundTransport) has(nodeID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.supervisors[nodeID]
	return ok
}

// stopAll cancels every supervisor and waits for all goroutines to exit.
// Synchronous teardown so the caller (Manager.Stop) can guarantee that no
// outbound goroutine outlives the shutdown defer chain.
func (t *outboundTransport) stopAll() {
	t.mu.Lock()
	t.stopped = true
	cancels := make([]context.CancelFunc, 0, len(t.supervisors))
	for _, entry := range t.supervisors {
		cancels = append(cancels, entry.cancel)
	}
	stopped := len(t.supervisors)
	t.supervisors = map[string]*outboundSupervisorEntry{}
	fn := t.onSupervisorDelta
	t.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	if fn != nil && stopped > 0 {
		fn(float64(-stopped))
	}
	t.wg.Wait()
}
