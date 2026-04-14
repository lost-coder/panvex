package server

import "net/http"

type versionResponse struct {
	Version   string `json:"version"`
	CommitSHA string `json:"commit_sha"`
	BuildTime string `json:"build_time"`
}

func (s *Server) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, versionResponse{
			Version:   s.version,
			CommitSHA: s.commitSHA,
			BuildTime: s.buildTime,
		})
	}
}
