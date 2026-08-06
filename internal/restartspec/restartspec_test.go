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
