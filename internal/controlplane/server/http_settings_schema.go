package server

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/settings"
)

// handleSettingsSchemaGET serves the registry schema for the dashboard.
func (s *Server) handleSettingsSchemaGET(w http.ResponseWriter, r *http.Request) {
	body, err := settings.RenderSchemaJSON()
	if err != nil {
		http.Error(w, "settings schema render failed", http.StatusInternalServerError)
		return
	}

	// Compute strong ETag from body
	etag := computeETag(body)

	// Check If-None-Match and return 304 if matched
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Set common headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("ETag", etag)

	// Apply gzip compression if requested
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		_, _ = gz.Write(body)
	} else {
		_, _ = w.Write(body)
	}
}

// computeETag returns a strong ETag for the given body: SHA256 hex-encoded and quoted.
func computeETag(body []byte) string {
	hash := sha256.Sum256(body)
	return `"` + hex.EncodeToString(hash[:]) + `"`
}
