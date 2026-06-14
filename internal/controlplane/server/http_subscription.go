package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// handleSubscriptionPage serves GET /sub/{token}. Public — the token is the
// only credential. Gates on the client being enabled and unexpired.
func (s *Server) handleSubscriptionPage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")

		token := chi.URLParam(r, "token")
		client, err := s.clientsSvc.ResolveBySubscriptionToken(r.Context(), token)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				s.writeSubscriptionInactive(w, http.StatusNotFound)
				return
			}
			s.logger.ErrorContext(r.Context(), "subscription resolve failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if !subscriptionClientActive(client, s.now()) {
			s.writeSubscriptionInactive(w, http.StatusForbidden)
			return
		}

		view, err := s.buildSubscriptionView(r.Context(), client)
		if err != nil {
			s.logger.ErrorContext(r.Context(), "subscription view build failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := subscriptionTemplate.Execute(w, view); err != nil {
			s.logger.ErrorContext(r.Context(), "subscription render failed", "err", err)
		}
	}
}

// subscriptionClientActive reports whether the client may see proxies: enabled
// and (if an expiration is set) not past it. A blank/invalid expiration is
// treated as "no expiry".
func subscriptionClientActive(client clients.Client, now time.Time) bool {
	if !client.Enabled {
		return false
	}
	if client.ExpirationRFC3339 == "" {
		return true
	}
	exp, err := time.Parse(time.RFC3339, client.ExpirationRFC3339)
	if err != nil {
		return true
	}
	return now.Before(exp)
}

func (s *Server) writeSubscriptionInactive(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="ru"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<meta name="robots" content="noindex"><title>Подписка неактивна</title></head>` +
		`<body style="background:#0b0d11;color:#eef2f8;font-family:sans-serif;text-align:center;padding:48px">` +
		`<h1>Подписка неактивна</h1><p>Ссылка недействительна или срок действия истёк. Обратитесь к администратору.</p></body></html>`))
}
