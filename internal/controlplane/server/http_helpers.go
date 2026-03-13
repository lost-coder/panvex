package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type errorResponse struct {
	Error string `json:"error"`
}

func decodeJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func newSequenceID(prefix string, value uint64) string {
	return prefix + "-" + leftPad(value)
}

func leftPad(value uint64) string {
	return fmt.Sprintf("%07d", value)
}
