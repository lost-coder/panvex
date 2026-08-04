package server

import "testing"

func TestParseReloadPolicy(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		timeout int
		wantErr bool
	}{
		{"empty defaults to drain-30", "", 0, false},
		{"instant ok", "instant", 0, false},
		{"instant with timeout rejected", "instant", 30, true},
		{"drain requires timeout", "drain", 0, true},
		{"drain timeout in range", "drain", 30, false},
		{"drain timeout too low", "drain", 0, true},
		{"drain timeout too high", "drain", 3601, true},
		{"unknown mode rejected", "reboot", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := parseReloadPolicy(c.mode, c.timeout)
			if c.wantErr != (err != nil) {
				t.Fatalf("parseReloadPolicy(%q,%d) err=%v wantErr=%v", c.mode, c.timeout, err, c.wantErr)
			}
			if err == nil && c.mode == "" {
				if p.Mode != "drain" || p.TimeoutSecs != 30 {
					t.Fatalf("empty policy = %+v, want drain/30", p)
				}
			}
		})
	}
}
