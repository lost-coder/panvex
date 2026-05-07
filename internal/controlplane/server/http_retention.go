package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) handleGetRetentionSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		writeJSON(w, http.StatusOK, s.retentionSettings())
	}
}

func (s *Server) handlePutRetentionSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req RetentionSettings
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		settings := normalizeRetentionSettings(req)

		// Marshal the canonical JSON before writing so the OperationalStore
		// and the in-memory snapshot are always consistent.
		canonicalJSON, err := json.Marshal(settings)
		if err != nil {
			s.logger.Error("marshal retention settings", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// Persist first, then update in-memory under the same mutex. If the
		// write fails we keep the previous in-memory value untouched so the
		// process never returns 200 on a write that didn't reach the database.
		// (P2-REL-03 / M-R1)
		//
		// Dual-write: s.settings.Put keeps the OperationalStore cache and the
		// panel_settings."default" scope in sync (read by /api/settings/values).
		// s.store.PutRetentionSettings keeps the panel_settings."panel" scope in
		// sync (read by restoreRetentionSettings at boot).
		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		if s.settings != nil {
			who := fmt.Sprintf("user:%s", session.UserID)
			if err := s.settings.Put(r.Context(), map[string]string{"retention": string(canonicalJSON)}, who); err != nil {
				s.logger.Error("put retention settings via OperationalStore failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		if s.store != nil {
			if err := s.store.PutRetentionSettings(r.Context(), retentionSettingsToRecord(settings)); err != nil {
				s.logger.Error("put retention settings failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		s.retention = settings

		writeJSON(w, http.StatusOK, settings)
	}
}

func normalizeRetentionSettings(s RetentionSettings) RetentionSettings {
	if s.TSRawSeconds <= 0 {
		s.TSRawSeconds = 86400
	}
	if s.TSHourlySeconds <= 0 {
		s.TSHourlySeconds = 604800
	}
	if s.TSDCSeconds <= 0 {
		s.TSDCSeconds = 86400
	}
	if s.IPHistorySeconds <= 0 {
		s.IPHistorySeconds = 2592000
	}
	if s.EventSeconds <= 0 {
		s.EventSeconds = 86400
	}
	if s.AuditEventSeconds <= 0 {
		s.AuditEventSeconds = 7776000
	}
	if s.MetricSnapshotSeconds <= 0 {
		s.MetricSnapshotSeconds = 2592000
	}
	return s
}
