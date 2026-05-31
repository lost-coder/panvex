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

func newJSONLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	h := logutil.NewHandler(logutil.Options{
		Format: logutil.FormatJSON,
		Level:  slog.LevelDebug,
		Sink:   &buf,
	})
	return slog.New(h), &buf
}

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("json not parseable: %v\noutput: %s", err, line)
	}
	return entry
}

func TestRedactTopLevelSensitiveAttr(t *testing.T) {
	lg, buf := newJSONLogger(t)
	lg.LogAttrs(context.Background(), slog.LevelInfo, "login",
		slog.String("password", "hunter2"),
		slog.String("username", "alice"),
	)

	entry := decodeLine(t, buf)
	if entry["password"] != "[REDACTED]" {
		t.Fatalf("password not redacted: %v", entry["password"])
	}
	if entry["username"] != "alice" {
		t.Fatalf("non-sensitive attr was altered: %v", entry["username"])
	}
}

func TestRedactInsideGroup(t *testing.T) {
	lg, buf := newJSONLogger(t)
	lg.LogAttrs(context.Background(), slog.LevelInfo, "auth",
		slog.Group("creds",
			slog.String("api_key", "sk-live-123"),
			slog.String("scope", "read"),
		),
	)

	entry := decodeLine(t, buf)
	creds, ok := entry["creds"].(map[string]any)
	if !ok {
		t.Fatalf("creds group missing or wrong type: %v", entry["creds"])
	}
	if creds["api_key"] != "[REDACTED]" {
		t.Fatalf("grouped api_key not redacted: %v", creds["api_key"])
	}
	if creds["scope"] != "read" {
		t.Fatalf("non-sensitive grouped attr altered: %v", creds["scope"])
	}
}

func TestRedactCaseInsensitiveAndSubstring(t *testing.T) {
	lg, buf := newJSONLogger(t)
	lg.LogAttrs(context.Background(), slog.LevelInfo, "conn",
		slog.String("Authorization", "Bearer abc"),
		slog.String("proxy_secret", "deadbeef"),
		slog.String("storage_dsn", "postgres://u:p@h/db"),
		slog.String("Session-ID", "sess-xyz"),
	)

	entry := decodeLine(t, buf)
	for _, k := range []string{"Authorization", "proxy_secret", "storage_dsn", "Session-ID"} {
		if entry[k] != "[REDACTED]" {
			t.Fatalf("%s not redacted: %v", k, entry[k])
		}
	}
}

func TestRedactWithAttrs(t *testing.T) {
	lg, buf := newJSONLogger(t)
	child := lg.With(slog.String("token", "t-secret"), slog.String("component", "agent"))
	child.LogAttrs(context.Background(), slog.LevelInfo, "tick")

	entry := decodeLine(t, buf)
	if entry["token"] != "[REDACTED]" {
		t.Fatalf("With() token not redacted: %v", entry["token"])
	}
	if entry["component"] != "agent" {
		t.Fatalf("With() non-sensitive attr altered: %v", entry["component"])
	}
}

func TestRedactDoesNotBreakRequestID(t *testing.T) {
	lg, buf := newJSONLogger(t)
	ctx := enrollment.WithRequestID(context.Background(), "rid-redact")
	lg.LogAttrs(ctx, slog.LevelInfo, "hello",
		slog.String("secret", "s"),
		slog.String("k", "v"),
	)

	entry := decodeLine(t, buf)
	if entry["request_id"] != "rid-redact" {
		t.Fatalf("request_id lost after redaction: %v", entry["request_id"])
	}
	if entry["secret"] != "[REDACTED]" {
		t.Fatalf("secret not redacted: %v", entry["secret"])
	}
	if entry["k"] != "v" {
		t.Fatalf("plain attr altered: %v", entry["k"])
	}
}
