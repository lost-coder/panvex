package server

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/coder/websocket"
)

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

		events, cancel := s.events.Subscribe()
		defer cancel()

		ctx := r.Context()
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

// wsOriginPatterns returns the allowed WebSocket origin patterns for the given
// request. When the request arrives from a loopback address the dev proxy port
// (e.g. Vite on :5173) may differ from the backend port, so we allow any port
// on loopback hosts in addition to the exact request host.
func (s *Server) wsOriginPatterns(r *http.Request) []string {
	patterns := []string{r.Host}

	remoteHost, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteHost == "127.0.0.1" || remoteHost == "::1" {
		// Allow any port from loopback connections (development)
		patterns = append(patterns, "127.0.0.1:*", "localhost:*", "::1:*")
	}

	return patterns
}

func mustJSON(payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"type":"server.error","data":{"error":"event encoding failed"}}`)
	}

	return data
}
