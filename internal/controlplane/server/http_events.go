package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// wsInboundFrameLimit caps the size of any single frame the server is
// willing to read from a connected client on /events (S6). The endpoint
// is server-to-client only: the browser subscribes and receives events,
// it never sends application data. We still have to drain the reader so
// the library processes Pong/Close control frames, but any data frame
// on this channel is abuse — capping the frame size keeps an attacker
// from parking a large slow-to-drain frame in the TCP buffer.
const wsInboundFrameLimit = 1 << 10 // 1 KiB

// wsWriteTimeout caps how long a single server→client frame may sit in
// the TCP buffer before we treat the reader as wedged and drop the
// connection (Q2.U-P-11). coder/websocket has no SetWriteDeadline; we
// wrap every Write in a per-call context.WithTimeout instead. 10s is
// generous: a healthy reader drains in microseconds, while a slow or
// hung peer should not block the event-bus broadcaster goroutine.
const wsWriteTimeout = 10 * time.Second

// EnvWSDevLoopback opts into the development-only behaviour that allows
// WebSocket upgrade requests from any port on 127.0.0.1/::1/localhost.
// Without this env set we fall back to the strict policy: the Origin must
// match the exact request Host, even for loopback clients. The Vite dev
// server proxies /api to :8080 with Origin matching :5173, so developers
// must set PANVEX_WS_DEV_LOOPBACK=1 explicitly when running split-port.
const EnvWSDevLoopback = "PANVEX_WS_DEV_LOOPBACK"

func (s *Server) handleEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Cap concurrent /events connections per user (or per-IP for the
		// unauthenticated path; today the requireSession check above
		// rejects unauth'd callers, but we still consult the IP key as
		// defence-in-depth in case the auth gate is moved). 429 is
		// returned BEFORE the WebSocket upgrade handshake so the client
		// observes a normal HTTP error rather than an upgraded socket
		// that immediately closes.
		limitKey, limit := s.eventsConnLimitKey(r, session.UserID)
		if !s.wsConnLimiter.acquire(limitKey, limit) {
			writeError(w, http.StatusTooManyRequests, "too many concurrent connections")
			return
		}
		defer s.wsConnLimiter.release(limitKey)

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: s.wsOriginPatterns(r),
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()

		// S6: cap per-frame payload size so a client cannot park a
		// slow-to-drain frame in our read buffer. SetReadLimit applies
		// to every subsequent read, including frames processed by the
		// library's control-frame handler.
		conn.SetReadLimit(wsInboundFrameLimit)

		events, cancel := s.events.Subscribe()
		defer cancel()

		ctx, cancelCtx := context.WithCancel(r.Context())
		defer cancelCtx()

		// S6: run a dedicated reader so the library can process Pong
		// and Close control frames and so that the connection does not
		// silently wedge. /events is strictly server-to-client, so any
		// data frame we read is treated as abuse: we close with
		// StatusPolicyViolation and let the defer in the main loop
		// finish shutdown. Errors from Read (EOF, client close, ctx
		// cancel, SetReadLimit overflow) just terminate this goroutine.
		go func() {
			defer cancelCtx()
			// One read is enough: this endpoint is strictly server→client, so
			// any data frame counts as abuse. The loop is gone (staticcheck
			// SA4004) — every path terminates after the first Read.
			msgType, _, err := conn.Reader(ctx)
			if err != nil {
				// context.Canceled, EOF, client-close, SetReadLimit overflow —
				// all just stop this goroutine.
				return
			}
			_ = msgType
			_ = conn.Close(websocket.StatusPolicyViolation, "client frames not accepted")
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}

				payload := event.Raw
				if len(payload) == 0 {
					// Fallback for backends that do not pre-marshal
					// (test fakes wired via NewHubWithBackend).
					payload = mustJSON(event)
				}
				writeCtx, cancelWrite := context.WithTimeout(ctx, wsWriteTimeout)
				err := conn.Write(writeCtx, websocket.MessageText, payload)
				cancelWrite()
				if err != nil {
					// Either a normal close, a slow reader hitting our
					// per-frame deadline, or the parent ctx going away.
					// All paths terminate the writer goroutine.
					return
				}
			}
		}
	}
}

// eventsConnLimitKey picks the conn-counter key + cap for a /events
// request. Authenticated callers (the common case) are keyed by their
// user-id with maxWSConnsPerUser. If a future refactor exposes /events
// to unauthenticated traffic the per-IP fallback applies a much higher
// maxWSConnsPerIP cap. The "user:" / "ip:" prefix mirrors the rate-limit
// key convention so a user-id whose literal value happens to equal an
// IP address cannot collide.
func (s *Server) eventsConnLimitKey(r *http.Request, userID string) (string, int32) {
	if userID != "" {
		return "user:" + userID, int32(maxWSConnsPerUser)
	}
	return "ip:" + s.requestClientRateLimitKey(r), int32(maxWSConnsPerIP)
}

// wsOriginPatterns returns the allowed WebSocket origin patterns for the
// given request. The default policy is strict: the Origin must match the
// exact request Host. Developers running the Vite dev server on a different
// port (e.g. :5173 -> :8080) must opt in by setting EnvWSDevLoopback=1;
// only then do we broaden the policy to allow any port on loopback hosts.
// This keeps production deployments fail-closed without a hidden escape
// hatch that a misconfigured sidecar could exploit.
func (s *Server) wsOriginPatterns(r *http.Request) []string {
	patterns := []string{r.Host}

	if !wsDevLoopbackEnabled() {
		return patterns
	}

	remoteHost, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteHost == "127.0.0.1" || remoteHost == "::1" {
		patterns = append(patterns, "127.0.0.1:*", "localhost:*", "::1:*")
	}
	return patterns
}

func wsDevLoopbackEnabled() bool {
	// L-11: refuse to honour the dev-loopback opt-in when PANVEX_ENV
	// signals production. Operators sometimes copy a dev .env into a
	// prod box and the loose origin policy is exactly the kind of
	// regression that should not survive that mistake.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("PANVEX_ENV")), "production") {
		return false
	}
	switch os.Getenv(EnvWSDevLoopback) {
	case "1", "true", "TRUE", "yes", "on":
		return true
	default:
		return false
	}
}

func mustJSON(payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"type":"server.error","data":{"error":"event encoding failed"}}`)
	}

	return data
}
