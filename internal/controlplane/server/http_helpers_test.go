package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrorLoggedClient400LogsAtWarn(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	w := httptest.NewRecorder()
	writeErrorLogged(context.Background(), w, 400, "bad input", errors.New("token malformed: parse failure"))

	if w.Code != 400 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	// Accept either {error: "..."} or {message: "..."} — adapt to actual writeError shape
	if resp["error"] != "bad input" && resp["message"] != "bad input" {
		t.Fatalf("client got internal detail: %v", resp)
	}

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Fatalf("expected WARN, got: %s", out)
	}
	if !strings.Contains(out, "token malformed") {
		t.Fatalf("internal error not logged: %s", out)
	}
}

func TestWriteErrorLoggedServer500LogsAtError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	w := httptest.NewRecorder()
	writeErrorLogged(context.Background(), w, 500, "internal error", errors.New("db connect refused"))

	out := buf.String()
	if !strings.Contains(out, "level=ERROR") {
		t.Fatalf("expected ERROR, got: %s", out)
	}
	if !strings.Contains(out, "db connect refused") {
		t.Fatalf("server error not logged: %s", out)
	}
}
