package logutil_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/logutil"
)

func TestNewHandlerTextEmitsKeyValue(t *testing.T) {
	var buf bytes.Buffer
	h := logutil.NewHandler(logutil.Options{
		Format: logutil.FormatText,
		Level:  slog.LevelDebug,
		Sink:   &buf,
	})
	lg := slog.New(h)
	ctx := enrollment.WithRequestID(context.Background(), "rid-test")

	lg.LogAttrs(ctx, slog.LevelInfo, "hello", slog.String("k", "v"))

	out := buf.String()
	if !strings.Contains(out, "msg=hello") && !strings.Contains(out, "msg=\"hello\"") {
		t.Fatalf("text output missing msg: %q", out)
	}
	if !strings.Contains(out, "request_id=rid-test") {
		t.Fatalf("text output missing request_id: %q", out)
	}
	if !strings.Contains(out, "k=v") {
		t.Fatalf("text output missing custom attr: %q", out)
	}
}

func TestNewHandlerJSONEmitsObject(t *testing.T) {
	var buf bytes.Buffer
	h := logutil.NewHandler(logutil.Options{
		Format: logutil.FormatJSON,
		Level:  slog.LevelDebug,
		Sink:   &buf,
	})
	lg := slog.New(h)
	ctx := enrollment.WithRequestID(context.Background(), "rid-json")

	lg.LogAttrs(ctx, slog.LevelWarn, "warn-msg", slog.Int("n", 7))

	line := strings.TrimSpace(buf.String())
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("json output not parseable: %v\noutput: %s", err, line)
	}
	if entry["msg"] != "warn-msg" {
		t.Fatalf("msg = %v", entry["msg"])
	}
	if entry["level"] != "WARN" {
		t.Fatalf("level = %v", entry["level"])
	}
	if entry["request_id"] != "rid-json" {
		t.Fatalf("request_id = %v", entry["request_id"])
	}
	if int(entry["n"].(float64)) != 7 {
		t.Fatalf("n = %v", entry["n"])
	}
}
