// Package logutil constructs the project's shared slog handler. Both
// cmd/control-plane and cmd/agent route through NewHandler so the format,
// level, context-propagation, and redaction layers stay aligned.
package logutil

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Format selects the wire-level encoding of slog records.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// ErrUnknownFormat is returned by ParseFormat when the input is not a
// recognised format. Callers should error out at startup rather than fall
// back silently — log format is operator-configured and a typo should be
// surfaced.
var ErrUnknownFormat = errors.New("unknown log format")

// ParseFormat is a case-insensitive lookup that maps "" to FormatText so
// the flag's default value yields the legacy behaviour.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownFormat, s)
	}
}

// Options describes how NewHandler should build the slog handler chain.
type Options struct {
	// Format selects the slog encoder.
	Format Format
	// Level is the minimum level emitted.
	Level slog.Level
	// Sink is the io.Writer that receives serialised records. nil falls
	// back to os.Stderr.
	Sink io.Writer
}

// resolveSink applies the os.Stderr default.
func (o Options) resolveSink() io.Writer {
	if o.Sink != nil {
		return o.Sink
	}
	return os.Stderr
}
