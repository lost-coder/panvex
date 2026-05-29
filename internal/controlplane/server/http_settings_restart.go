package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleSettingsRestartStatusGET(w http.ResponseWriter, r *http.Request) {
	var pending []string
	if s.settings != nil {
		pending = s.settings.PendingChanges(s.settingsActive)
	}
	resp := struct {
		Pending bool     `json:"pending"`
		Fields  []string `json:"fields"`
	}{
		Pending: len(pending) > 0,
		Fields:  pending,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
