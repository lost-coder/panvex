package state

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestConcurrentFieldWritersAndRenewalUpdate reproduces audit #7: a
// high-frequency state-file writer (Update on one field) racing a
// certificate renewal must never resurrect the OLD cert on disk. Run
// with -race.
func TestConcurrentFieldWritersAndRenewalUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	initial := Credentials{
		AgentID:        "agent-1",
		CertificatePEM: "OLD-CERT",
		PrivateKeyPEM:  "OLD-KEY",
	}
	if err := Save(path, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := Update(path, func(c *Credentials) {
				c.TransportSwitchedAtUnix = int64(i) //nolint:gosec
			}); err != nil {
				t.Errorf("Update(TransportSwitchedAtUnix=%d): %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			_, err := Update(path, func(c *Credentials) {
				c.CertificatePEM = "NEW-CERT"
				c.PrivateKeyPEM = "NEW-KEY"
			})
			if err != nil {
				t.Errorf("Update(renewal): %v", err)
			}
		}()
	}
	wg.Wait()

	final, err := Load(path)
	if err != nil {
		t.Fatalf("Load final: %v", err)
	}
	if final.CertificatePEM != "NEW-CERT" || final.PrivateKeyPEM != "NEW-KEY" {
		t.Fatalf("renewal lost: cert=%q key=%q, want NEW-CERT/NEW-KEY (field writer clobbered the renewal)", final.CertificatePEM, final.PrivateKeyPEM)
	}
}
