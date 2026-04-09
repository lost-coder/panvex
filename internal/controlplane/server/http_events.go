package server

import (
	"encoding/json"
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
			OriginPatterns: []string{r.Host},
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()

		events, cancel := s.events.subscribe()
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

func mustJSON(payload any) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"type":"server.error","data":{"error":"event encoding failed"}}`)
	}

	return data
}
