package agenttransport

import (
	"context"
	"crypto/tls"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

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
		if err := s.connectAndServe(ctx); err != nil {
			s.logger.Warn("agenttransport: outbound session ended",
				"node_id", s.meta.NodeID, "addr", s.meta.DialAddress, "error", err)
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
// supervisors maps nodeID to a live supervisor entry; each entry holds the
// cancel function for that supervisor's context.
type outboundTransport struct {
	tlsCfg  *tls.Config
	handler SessionHandler
	logger  *slog.Logger

	mu          sync.RWMutex
	supervisors map[string]*outboundSupervisorEntry
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
	t.mu.Unlock()

	sup := newOutboundSupervisor(meta, t.tlsCfg, t.handler, t.logger)
	go sup.run(ctx)
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
}
