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
	inner    slog.Handler
	buf      *Buffer
	onUrgent func() // optional, fired after Warn/Error is buffered; non-blocking

	// Bound state copied on WithAttrs/WithGroup so that records buffered
	// for shipment to the panel include the same attrs the inner handler
	// sees on stderr.
	attrs []slog.Attr
	group string // prefix applied to BOTH attrs and record-local fields
}

func NewHandler(inner slog.Handler, buf *Buffer) *Handler {
	return &Handler{inner: inner, buf: buf}
}

// SetUrgentCallback installs a callback fired after a Warn or Error record
// is buffered. The callback must be non-blocking; the only legitimate
// implementation is a select-default send onto a notify channel.
func (h *Handler) SetUrgentCallback(fn func()) {
	h.onUrgent = fn
}

func (h *Handler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var isUrgent bool
	if r.Level >= slog.LevelInfo {
		fields := h.collectFields(r)
		h.buf.Append(Event{
			Ts:      r.Time,
			Level:   levelString(r.Level),
			Message: r.Message,
			Fields:  fields,
		})
		isUrgent = r.Level >= slog.LevelWarn
	}
	err := h.inner.Handle(ctx, r)
	if isUrgent && h.onUrgent != nil {
		h.onUrgent()
	}
	return err
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.inner = h.inner.WithAttrs(attrs)
	if len(attrs) > 0 {
		cp.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	}
	return &cp
}

func (h *Handler) WithGroup(name string) slog.Handler {
	cp := *h
	cp.inner = h.inner.WithGroup(name)
	if name != "" {
		if cp.group == "" {
			cp.group = name
		} else {
			cp.group = cp.group + "." + name
		}
	}
	return &cp
}

// collectFields merges Handler-bound attrs with the record's own attrs into
// a single string-valued map. Group prefix is applied to all keys.
func (h *Handler) collectFields(r slog.Record) map[string]string {
	fields := make(map[string]string, len(h.attrs)+r.NumAttrs())
	for _, a := range h.attrs {
		fields[h.keyWithGroup(a.Key)] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[h.keyWithGroup(a.Key)] = a.Value.String()
		return true
	})
	return fields
}

func (h *Handler) keyWithGroup(key string) string {
	if h.group == "" {
		return key
	}
	return h.group + "." + key
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
