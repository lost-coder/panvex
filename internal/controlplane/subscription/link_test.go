package subscription

import "testing"

func TestParseLink(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantDom  string
		wantPort string
		wantMode string
		wantOK   bool
	}{
		{"faketls https", "https://t.me/proxy?server=nl1.cdn.com&port=443&secret=ee1234abcd", "nl1.cdn.com", "443", "FakeTLS", true},
		{"secure tg", "tg://proxy?server=de1.host.net&port=8443&secret=dd9988", "de1.host.net", "8443", "Secure", true},
		{"classic", "tg://proxy?server=fi1.edge.io&port=80&secret=abcdef", "fi1.edge.io", "80", "Classic", true},
		{"garbage", "not a url at all %%%", "", "", "", false},
		{"missing server", "https://t.me/proxy?port=443&secret=ee00", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseLink(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Domain != tc.wantDom || got.Port != tc.wantPort || got.Mode != tc.wantMode {
				t.Fatalf("got %+v, want dom=%s port=%s mode=%s", got, tc.wantDom, tc.wantPort, tc.wantMode)
			}
			if got.Raw != tc.raw {
				t.Fatalf("Raw = %q, want %q", got.Raw, tc.raw)
			}
		})
	}
}
