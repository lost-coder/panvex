package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/requestid"
)

func TestRequestIDMiddleware_GeneratesWhenAbsent(t *testing.T) {
	var captured string
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = requestid.FromContext(r.Context())
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if captured == "" {
		t.Fatal("ctx request id should be set")
	}
	if rec.Header().Get(requestIDHeader) != captured {
		t.Fatalf("response header %q != ctx %q", rec.Header().Get(requestIDHeader), captured)
	}
}

func TestRequestIDMiddleware_HonoursClientHeader(t *testing.T) {
	const supplied = "trace-abc-123"
	var captured string
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = requestid.FromContext(r.Context())
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, supplied)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if captured != supplied {
		t.Fatalf("ctx id = %q, want %q", captured, supplied)
	}
	if rec.Header().Get(requestIDHeader) != supplied {
		t.Fatalf("response header = %q, want %q", rec.Header().Get(requestIDHeader), supplied)
	}
}

func TestRequestIDMiddleware_RejectsMalformedClientHeader(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"contains space",
		"line\nbreak",
		"control\x07char",
		strings.Repeat("a", 129), // over 128 bytes
	}
	for _, supplied := range cases {
		t.Run(supplied, func(t *testing.T) {
			var captured string
			handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = requestid.FromContext(r.Context())
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			if supplied != "" {
				req.Header.Set(requestIDHeader, supplied)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if captured == "" {
				t.Fatal("expected fallback ID generated")
			}
			if captured == supplied {
				t.Fatalf("malformed supplied id should not be accepted: %q", supplied)
			}
		})
	}
}
