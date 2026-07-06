package metrics

import "testing"

func TestStatusBucket(t *testing.T) {
	cases := []struct {
		code int
		want string
	}{
		{200, "2xx"},
		{204, "2xx"},
		{301, "3xx"},
		{404, "4xx"},
		{429, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{100, "100"}, // falls outside 2xx-5xx; raw code kept so oddities are visible
	}

	for _, c := range cases {
		if got := statusBucket(c.code); got != c.want {
			t.Errorf("statusBucket(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}
