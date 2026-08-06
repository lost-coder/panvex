package restartspec

import (
	"errors"
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		spec    string
		want    []string
		wantErr bool
	}{
		{spec: "systemd:telemt", want: []string{"systemctl", "restart", "telemt"}},
		{spec: "procd:telemt", want: []string{"/etc/init.d/telemt", "restart"}},
		{spec: "openrc:telemt", want: []string{"rc-service", "telemt", "restart"}},
		{spec: "runit:telemt", want: []string{"sv", "restart", "telemt"}},
		{spec: "command:/usr/local/bin/restart-telemt.sh --fast", want: []string{"/usr/local/bin/restart-telemt.sh", "--fast"}},
		{spec: "  systemd:telemt  ", want: []string{"systemctl", "restart", "telemt"}},
		{spec: "systemd:", wantErr: true},
		{spec: "unknown:x", wantErr: true},
		{spec: "command:   ", wantErr: true},
		{spec: "systemd", wantErr: true}, // нет ':'
	}
	for _, tc := range tests {
		got, err := Parse(tc.spec)
		if tc.wantErr != (err != nil) {
			t.Fatalf("Parse(%q) err=%v, wantErr=%v", tc.spec, err, tc.wantErr)
		}
		if err == nil && !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("Parse(%q)=%v want %v", tc.spec, got, tc.want)
		}
	}
}

func TestParseEmptyIsTypedError(t *testing.T) {
	if _, err := Parse("   "); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
}

// TestParseRejectsUnsafeNames exercises validateName's three reject branches
// (whitespace, "/", "..") directly, since the substituted name ends up in a
// fixed argv (and, for procd, a filesystem path): an unvalidated name would
// let an operator-supplied spec smuggle in extra arguments or escape
// /etc/init.d via path traversal.
func TestParseRejectsUnsafeNames(t *testing.T) {
	specs := []string{
		"systemd:foo bar", // whitespace
		"systemd:foo/bar", // '/'
		"systemd:../etc",  // '..'
		"procd:foo bar",   // whitespace, procd branch
		"procd:foo/bar",   // '/', procd branch
		"procd:../etc",    // '..', procd branch (path traversal into /etc/init.d/..)
		"openrc:foo bar",  // whitespace, openrc branch
		"openrc:foo/bar",  // '/', openrc branch
		"openrc:../etc",   // '..', openrc branch
		"runit:foo bar",   // whitespace, runit branch
		"runit:foo/bar",   // '/', runit branch
		"runit:../etc",    // '..', runit branch
	}
	for _, spec := range specs {
		if _, err := Parse(spec); err == nil {
			t.Fatalf("Parse(%q): want error, got nil", spec)
		}
	}
}

// TestParseCommandAllowsSlashesAndSpaces confirms the name-validation
// restrictions above are specific to the fixed-argv presets: "command:"
// legitimately needs '/' (paths) and spaces (argument separators), and must
// keep working unrestricted.
func TestParseCommandAllowsSlashesAndSpaces(t *testing.T) {
	got, err := Parse("command:/usr/local/bin/restart-telemt.sh --fast --verbose")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"/usr/local/bin/restart-telemt.sh", "--fast", "--verbose"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse=%v want %v", got, want)
	}
}
