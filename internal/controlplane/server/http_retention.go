package server

import (
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
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req RetentionSettings
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		settings := normalizeRetentionSettings(req)

		s.settingsMu.Lock()
		s.retention = settings
		s.settingsMu.Unlock()

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
	return s
}
