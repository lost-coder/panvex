package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// scrubErrorMessage returns a safe-to-expose form of err.Error()
// (Q3.U-S-26). Messages that contain anything resembling a secret
// (password, token, secret, ciphertext, private key) are collapsed to
// a generic "internal error" so a misformatted underlying error
// cannot leak secret material via the HTTP response. Callers should
// log the full error separately for diagnostics.
func scrubErrorMessage(message string) string {
	lower := strings.ToLower(message)
	for _, needle := range []string{"password", "secret", "token", "ciphertext", "private key", "passphrase"} {
		if strings.Contains(lower, needle) {
			return "internal error"
		}
	}
	return message
}

func writeErrorWithCode(w http.ResponseWriter, status int, message string, code string) {
	writeJSON(w, status, errorResponse{Error: scrubErrorMessage(message), Code: code})
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

// decodeJSON decodes the request body into dest with strict semantics
// (Q3.U-Q-05): unknown fields are rejected so client typos surface as
// 400 errors instead of being silently dropped, and trailing JSON
// after the first value is also rejected so a request body cannot
// smuggle a second payload past the handler.
func decodeJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing JSON after object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: scrubErrorMessage(message)})
}

func newSequenceID(prefix string, value uint64) string {
	return prefix + "-" + leftPad(value)
}

func leftPad(value uint64) string {
	return fmt.Sprintf("%07d", value)
}

// maskToken returns a truncated preview of a secret token for safe inclusion
// in audit logs and non-privileged responses.
func maskToken(value string) string {
	if len(value) <= 8 {
		return "***"
	}
	return value[:8] + "..."
}
