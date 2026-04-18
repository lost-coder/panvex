package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"

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

// EnvWSDevLoopback opts into the development-only behaviour that allows
// WebSocket upgrade requests from any port on 127.0.0.1/::1/localhost.
// Without this env set we fall back to the strict policy: the Origin must
// match the exact request Host, even for loopback clients. The Vite dev
// server proxies /api to :8080 with Origin matching :5173, so developers
// must set PANVEX_WS_DEV_LOOPBACK=1 explicitly when running split-port.
const EnvWSDevLoopback = "PANVEX_WS_DEV_LOOPBACK"

func (s *Server) handleEvents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

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
			for {
				msgType, _, err := conn.Reader(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					return
				}
				_ = msgType
				_ = conn.Close(websocket.StatusPolicyViolation, "client frames not accepted")
				return
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}

				if err := conn.Write(ctx, websocket.MessageText, mustJSON(event)); err != nil {
					if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
						return
					}
					return
				}
			}
		}
	}
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
