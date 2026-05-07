package server

import (
	"net/http"

	"github.com/lost-coder/panvex/internal/controlplane/settings"
)

// handleSettingsSchemaGET serves the registry schema for the dashboard.
func (s *Server) handleSettingsSchemaGET(w http.ResponseWriter, r *http.Request) {
	body, err := settings.RenderSchemaJSON()
	if err != nil {
		http.Error(w, "settings schema render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_, _ = w.Write(body)
}
