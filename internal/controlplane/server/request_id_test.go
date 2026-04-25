package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDMiddleware_GeneratesWhenAbsent(t *testing.T) {
	var captured string
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = requestIDFromContext(r.Context())
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
		captured = requestIDFromContext(r.Context())
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
				captured = requestIDFromContext(r.Context())
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

func TestSlogContextHandler_IncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newSlogContextHandler(slog.NewTextHandler(&buf, nil)))

	ctx := context.WithValue(context.Background(), requestIDKey{}, "req-42")
	logger.InfoContext(ctx, "hello")

	out := buf.String()
	if !strings.Contains(out, "request_id=req-42") {
		t.Fatalf("expected request_id=req-42 in log line, got: %q", out)
	}
}

func TestSlogContextHandler_NoIDNoAttribute(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newSlogContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.Info("hello")

	if strings.Contains(buf.String(), "request_id=") {
		t.Fatalf("should not emit empty request_id: %q", buf.String())
	}
}
