package webhooks

import "testing"

func TestMatchesFilter(t *testing.T) {
	cases := []struct {
		name   string
		action string
		filter []string
		want   bool
	}{
		{"empty filter matches all", "agent.unhealthy", nil, true},
		{"empty-string entries treated as empty", "agent.unhealthy", []string{"", " "}, false},
		{"exact match", "audit.user.login", []string{"audit.user.login"}, true},
		{"exact mismatch", "audit.user.login", []string{"audit.user.logout"}, false},
		{"prefix match", "agent.unhealthy", []string{"agent.*"}, true},
		{"prefix non-match", "agentless.foo", []string{"agent.*"}, false},
		{"prefix does not match bare prefix", "agent", []string{"agent.*"}, false},
		{"second pattern matches", "audit.security.bad", []string{"agent.*", "audit.security.*"}, true},
		{"deep nested prefix", "audit.user.totp.disabled", []string{"audit.user.*"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matchesFilter(c.action, c.filter); got != c.want {
				t.Errorf("matchesFilter(%q, %v) = %v, want %v", c.action, c.filter, got, c.want)
			}
		})
	}
}

func TestParseFilter(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"agent.*", []string{"agent.*"}},
		{"agent.*, audit.security.*", []string{"agent.*", "audit.security.*"}},
		{"  a , , , b ", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := parseFilter(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("parseFilter(%q) len = %d, want %d (got %v)", c.in, len(got), len(c.want), got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseFilter(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}
