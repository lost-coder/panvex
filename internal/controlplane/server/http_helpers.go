package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type errorResponse struct {
	Error string `json:"error"`
}

// maxRequestBodyBytes limits the size of incoming JSON request bodies.
const maxRequestBodyBytes = 1 << 20 // 1 MB

// maxBodySize applies a request body size limit as middleware, preventing
// oversized payloads from consuming server memory.
func maxBodySize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
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
