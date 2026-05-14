package runtimeevents

import (
	"context"
	"log/slog"
)

// Handler wraps an inner slog.Handler. It copies every Info+ record into
// a Buffer for later shipment to the panel, then delegates to the inner
// handler for the usual stderr / file output. Debug records bypass the
// buffer.
//
// Handler is safe for concurrent use if the inner handler is.
type Handler struct {
	inner slog.Handler
	buf   *Buffer
}

func NewHandler(inner slog.Handler, buf *Buffer) *Handler {
	return &Handler{inner: inner, buf: buf}
}

func (h *Handler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelInfo {
		h.buf.Append(Event{
			Ts:      r.Time,
			Level:   levelString(r.Level),
			Message: r.Message,
			Fields:  recordFields(r),
		})
	}
	return h.inner.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{inner: h.inner.WithAttrs(attrs), buf: h.buf}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{inner: h.inner.WithGroup(name), buf: h.buf}
}

func levelString(lvl slog.Level) string {
	switch {
	case lvl >= slog.LevelError:
		return "error"
	case lvl >= slog.LevelWarn:
		return "warn"
	default:
		return "info"
	}
}

func recordFields(r slog.Record) map[string]string {
	fields := make(map[string]string, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.String()
		return true
	})
	return fields
}
