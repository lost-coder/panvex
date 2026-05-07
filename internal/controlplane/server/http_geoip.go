package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

// geoipResponse is the shape returned by GET / PUT / refresh.
type geoipResponse struct {
	Settings geoip.Settings `json:"settings"`
	State    geoip.State    `json:"state"`
}

func (s *Server) handleGetGeoIPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		s.settingsMu.RLock()
		resp := geoipResponse{Settings: s.geoipSettings, State: s.geoipState}
		s.settingsMu.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	}
}

func (s *Server) handlePutGeoIPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req geoip.Settings
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := validateGeoIPSettings(req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		who := fmt.Sprintf("user:%s", session.UserID)
		s.settingsMu.Lock()
		prevMode := s.geoipSettings.Mode
		s.geoipSettings = req
		if err := s.persistGeoIPSettings(r.Context(), who); err != nil {
			s.settingsMu.Unlock()
			s.logger.Error("persist geoip settings", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// Mode-specific side effects.
		switch req.Mode {
		case geoip.ModeDisabled:
			_ = s.geoip.Close()
			s.geoipState = geoip.State{}
			s.persistGeoIPState(r.Context())
		case geoip.ModeLocal:
			s.reloadGeoIPManager()
		}
		s.settingsMu.Unlock()

		// Restart worker only on transitions in or out of an
		// auto/url mode. Within the same mode the running worker
		// re-reads settings each tick, so we don't churn it on URL
		// edits. startGeoIPUpdaterWorker self-takes settingsMu, so
		// it MUST be called with the lock dropped.
		isPolling := func(m geoip.Mode) bool { return m == geoip.ModeAuto || m == geoip.ModeURL }
		if isPolling(prevMode) != isPolling(req.Mode) {
			// Detached from r.Context() on purpose: the refresh worker
			// must outlive the request that toggled the mode; otherwise
			// closing the browser tab would tear the worker down on the
			// very tick that started it. Worker lifecycle is bound to
			// s.serverCtx so Shutdown still cancels it cleanly.
			//nolint:contextcheck // intentionally detached from request lifecycle
			s.startGeoIPUpdaterWorker(s.serverCtx)
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.geoip.update", "panel", map[string]any{
			"mode": req.Mode,
		})

		s.settingsMu.RLock()
		resp := geoipResponse{Settings: s.geoipSettings, State: s.geoipState}
		s.settingsMu.RUnlock()
		writeJSON(w, http.StatusOK, resp)
	}
}

func (s *Server) handleRefreshGeoIP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), geoipUpdateTimeout*2)
		defer cancel()

		// Run sequentially so individual error messages stay attached
		// to the right Kind.
		for _, k := range []geoip.Kind{geoip.KindCity, geoip.KindASN} {
			single, singleCancel := context.WithTimeout(ctx, geoipUpdateTimeout)
			newState := s.runGeoIPUpdate(single, k)
			singleCancel()
			s.settingsMu.Lock()
			if slot := s.geoipState.ForKind(k); slot != nil {
				*slot = newState
			}
			s.settingsMu.Unlock()
		}

		s.settingsMu.Lock()
		s.reloadGeoIPManager()
		s.persistGeoIPState(ctx)
		resp := geoipResponse{Settings: s.geoipSettings, State: s.geoipState}
		s.settingsMu.Unlock()

		s.appendAuditWithContext(r.Context(), session.UserID, "settings.geoip.refresh", "panel", map[string]any{
			"mode": resp.Settings.Mode,
			"at":   s.now().UTC().Format(time.RFC3339),
		})

		writeJSON(w, http.StatusOK, resp)
	}
}
