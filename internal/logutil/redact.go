package logutil

import (
	"context"
	"log/slog"
	"strings"
)

// redactedPlaceholder is substituted for the value of any attribute whose
// key matches the sensitive-key list. It is intentionally distinctive so an
// operator grepping logs can see that redaction fired.
const redactedPlaceholder = "[REDACTED]"

// sensitiveKeySubstrings is the case-insensitive deny-list of attribute-key
// fragments whose values must never reach a log sink in clear text. Matching
// is substring-based so compound keys (e.g. "db_password", "proxy_secret",
// "x_api_key") are caught without enumerating every variation.
//
// This is the defence-in-depth layer: callsite helpers
// (internal/controlplane/server logUsername / logIPHash / logSessionID and
// friends) remain the first line, but this handler guarantees a stray
// slog.String("password", ...) anywhere in the tree — including third-party
// library code — is masked regardless of caller discipline.
//
// Keep fragments lowercase; matchSensitiveKey lower-cases the candidate key
// before comparing.
var sensitiveKeySubstrings = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"session",
	"cookie",
	"authorization",
	"api_key",
	"apikey",
	"dsn",
	"credential",
	"private_key",
	"privatekey",
	"passphrase",
}

// matchSensitiveKey reports whether key (case-insensitive) contains any
// fragment from sensitiveKeySubstrings.
func matchSensitiveKey(key string) bool {
	if key == "" {
		return false
	}
	lower := strings.ToLower(key)
	for _, frag := range sensitiveKeySubstrings {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// redactingHandler is a slog.Handler middleware that walks every record's
// attributes (including nested groups and attributes accumulated via
// WithAttrs) and replaces the value of any sensitive-keyed attribute with
// redactedPlaceholder before delegating to the wrapped handler.
//
// It composes with slogContextHandler: install order is
//
//	encoder → redactingHandler → slogContextHandler
//
// so the request_id injected by slogContextHandler is added *after* redaction
// runs and is never itself masked (its key does not match the deny-list
// anyway). See NewHandler.
type redactingHandler struct {
	wrapped slog.Handler
}

// newRedactingHandler wraps inner. Returns nil when inner is nil so callers
// can compose unconditionally.
func newRedactingHandler(inner slog.Handler) slog.Handler {
	if inner == nil {
		return nil
	}
	return &redactingHandler{wrapped: inner}
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.wrapped.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	// Rebuild the record with redacted attributes. slog.Record stores its
	// own time/level/message/pc, so clone those and re-add the (possibly
	// rewritten) attribute set.
	clone := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(a slog.Attr) bool {
		clone.AddAttrs(redactAttr(a))
		return true
	})
	return h.wrapped.Handle(ctx, clone)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Redact attrs carried via logger.With(...) too — otherwise a sensitive
	// value bound once on a child logger would leak on every later record.
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &redactingHandler{wrapped: h.wrapped.WithAttrs(redacted)}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{wrapped: h.wrapped.WithGroup(name)}
}

// redactAttr returns a copy of a with its value masked when the key is
// sensitive, recursing into group values so nested attributes are covered.
func redactAttr(a slog.Attr) slog.Attr {
	if matchSensitiveKey(a.Key) {
		return slog.String(a.Key, redactedPlaceholder)
	}
	// Resolve LogValuers once so a Group/struct hiding behind a LogValuer is
	// still walked.
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		group := v.Group()
		redacted := make([]slog.Attr, len(group))
		for i, ga := range group {
			redacted[i] = redactAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	}
	return slog.Attr{Key: a.Key, Value: v}
}
