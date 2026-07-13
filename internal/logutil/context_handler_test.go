package logutil

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/requestid"
)

func TestSlogContextHandler_IncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewSlogContextHandler(slog.NewTextHandler(&buf, nil)))

	ctx := requestid.WithRequestID(context.Background(), "req-42")
	logger.InfoContext(ctx, "hello")

	if !strings.Contains(buf.String(), "request_id=req-42") {
		t.Fatalf("expected request_id=req-42 in log line, got: %q", buf.String())
	}
}

func TestSlogContextHandler_NoIDNoAttribute(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewSlogContextHandler(slog.NewTextHandler(&buf, nil)))

	logger.Info("hello")

	if strings.Contains(buf.String(), "request_id=") {
		t.Fatalf("should not emit empty request_id: %q", buf.String())
	}
}

func TestNewSlogContextHandler_NilInner(t *testing.T) {
	if NewSlogContextHandler(nil) != nil {
		t.Fatal("nil inner handler must return nil")
	}
}
