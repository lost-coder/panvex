package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPprofListenerEnabledFlag asserts the helper flag toggles based on
// whether SetPprofListenerAddr was called with a non-empty value. (S-07)
func TestPprofListenerEnabledFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		addr string
		want bool
	}{
		{"empty disables", "", false},
		{"whitespace disables", "   ", false},
		{"loopback enables", "127.0.0.1:6060", true},
		{"ipv6 loopback enables", "[::1]:6060", true},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{}
			s.SetPprofListenerAddr(tt.addr)
			if got := s.pprofListenerEnabled(); got != tt.want {
				t.Fatalf("pprofListenerEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStartPprofListenerErrorsWhenUnconfigured asserts that calling
// StartPprofListener without first opting in via SetPprofListenerAddr is a
// hard error — fail-closed instead of silently binding to a random address.
func TestStartPprofListenerErrorsWhenUnconfigured(t *testing.T) {
	t.Parallel()
	s := &Server{}
	addr, shutdown, err := s.StartPprofListener(context.Background())
	if err == nil {
		t.Fatal("StartPprofListener with no addr: nil error")
	}
	if addr != nil {
		t.Fatal("StartPprofListener with no addr: returned non-nil addr")
	}
	if shutdown != nil {
		t.Fatal("StartPprofListener with no addr: returned non-nil shutdown")
	}
}

// TestStartPprofListenerServesPprofIndex brings the dedicated listener up on
// a loopback ephemeral port and asserts /debug/pprof/ returns 200 with a
// recognisable pprof index body. Confirms the standalone wiring is real,
// not just dead code. (S-07)
func TestStartPprofListenerServesPprofIndex(t *testing.T) {
	s := &Server{}
	s.SetPprofListenerAddr("127.0.0.1:0")
	addr, shutdown, err := s.StartPprofListener(context.Background())
	if err != nil {
		t.Fatalf("StartPprofListener: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	url := "http://" + addr.String() + "/debug/pprof/"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	// pprof.Index emits an HTML index titled "/debug/pprof/" with links
	// to allocs/block/goroutine/heap/etc. Cheap content check.
	bodyStr := string(body)
	for _, marker := range []string{"goroutine", "heap", "/debug/pprof/"} {
		if !strings.Contains(bodyStr, marker) {
			t.Fatalf("body missing %q marker:\n%s", marker, bodyStr)
		}
	}
}
