package agenttransport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// errOutboundTLSMissing is returned by connectAndServe when no TLS config has
// been wired. Without it the panel would silently dial without mTLS, accepting
// any cert signed by a system CA — a security regression we surface loudly.
var errOutboundTLSMissing = errors.New("agenttransport: outbound TLS config is required but not set")

const (
	outboundBackoffInitial = 1 * time.Second
	outboundBackoffMax     = 60 * time.Second
)

// outboundSupervisor maintains a single agent's outbound (panel-dials-agent)
// gRPC connection with exponential backoff + jitter on disconnects.
type outboundSupervisor struct {
	meta           NodeMeta
	tlsCfg         *tls.Config
	handler        SessionHandler
	logger         *slog.Logger
	backoffInitial time.Duration
	backoffMax     time.Duration
}

func newOutboundSupervisor(meta NodeMeta, tlsCfg *tls.Config, h SessionHandler, l *slog.Logger) *outboundSupervisor {
	return &outboundSupervisor{
		meta:           meta,
		tlsCfg:         tlsCfg,
		handler:        h,
		logger:         l,
		backoffInitial: outboundBackoffInitial,
		backoffMax:     outboundBackoffMax,
	}
}

func (s *outboundSupervisor) run(ctx context.Context) {
	backoff := s.backoffInitial
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
		if time.Since(start) >= s.backoffMax {
			backoff = s.backoffInitial
		}
		delay := jitter(backoff)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < s.backoffMax {
			backoff *= 2
			if backoff > s.backoffMax {
				backoff = s.backoffMax
			}
		}
	}
}

func (s *outboundSupervisor) connectAndServe(ctx context.Context) error {
	if s.tlsCfg == nil {
		return fmt.Errorf("%w (node_id=%s)", errOutboundTLSMissing, s.meta.NodeID)
	}
	conn, err := grpc.NewClient(s.meta.DialAddress,
		grpc.WithTransportCredentials(credentials.NewTLS(s.tlsCfg)))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := gatewayrpc.NewAgentGatewayClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
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

// outboundTransport is the supervisor pool for outbound (reverse-mode) agents.
// supervisors maps nodeID to a live supervisor entry; wg tracks the spawned
// goroutines so stopAll can drain them synchronously.
type outboundTransport struct {
	tlsCfg  *tls.Config
	handler SessionHandler
	logger  *slog.Logger

	mu          sync.RWMutex
	supervisors map[string]*outboundSupervisorEntry
	wg          sync.WaitGroup
}

func newOutboundTransport(tlsCfg *tls.Config, handler SessionHandler, logger *slog.Logger) *outboundTransport {
	return &outboundTransport{
		tlsCfg:      tlsCfg,
		handler:     handler,
		logger:      logger,
		supervisors: map[string]*outboundSupervisorEntry{},
	}
}

func (t *outboundTransport) ensureSupervisor(meta NodeMeta) {
	t.mu.Lock()
	if _, exists := t.supervisors[meta.NodeID]; exists {
		t.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.supervisors[meta.NodeID] = &outboundSupervisorEntry{cancel: cancel}
	t.wg.Add(1)
	t.mu.Unlock()

	sup := newOutboundSupervisor(meta, t.tlsCfg, t.handler, t.logger)
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
	t.mu.Unlock()
	if ok {
		entry.cancel()
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
	cancels := make([]context.CancelFunc, 0, len(t.supervisors))
	for _, entry := range t.supervisors {
		cancels = append(cancels, entry.cancel)
	}
	t.supervisors = map[string]*outboundSupervisorEntry{}
	t.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	t.wg.Wait()
}
