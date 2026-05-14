package logutil_test

import (
	"errors"
	"testing"

	"github.com/lost-coder/panvex/internal/logutil"
)

func TestParseFormatAcceptsKnownValues(t *testing.T) {
	cases := map[string]logutil.Format{
		"text": logutil.FormatText,
		"json": logutil.FormatJSON,
		"":     logutil.FormatText,
		"TEXT": logutil.FormatText,
		"Json": logutil.FormatJSON,
	}
	for input, want := range cases {
		got, err := logutil.ParseFormat(input)
		if err != nil {
			t.Errorf("ParseFormat(%q) returned error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseFormat(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseFormatRejectsUnknown(t *testing.T) {
	_, err := logutil.ParseFormat("yaml")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !errors.Is(err, logutil.ErrUnknownFormat) {
		t.Fatalf("got %v, want ErrUnknownFormat wrap", err)
	}
}
