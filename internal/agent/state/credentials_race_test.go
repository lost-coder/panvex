package state

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestConcurrentUsageSeqAndRenewalUpdate reproduces audit #7: the
// usage-seq worker's Load→Save racing a certificate renewal must never
// resurrect the OLD cert on disk. Run with -race.
func TestConcurrentUsageSeqAndRenewalUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	initial := Credentials{
		AgentID:        "agent-1",
		CertificatePEM: "OLD-CERT",
		PrivateKeyPEM:  "OLD-KEY",
		UsageSeq:       1,
	}
	if err := Save(path, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		seq := uint64(i + 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := SaveUsageSeq(path, seq); err != nil {
				t.Errorf("SaveUsageSeq(%d): %v", seq, err)
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
		t.Fatalf("renewal lost: cert=%q key=%q, want NEW-CERT/NEW-KEY (usage-seq writer clobbered the renewal)", final.CertificatePEM, final.PrivateKeyPEM)
	}
	if final.UsageSeq < 2 {
		t.Fatalf("UsageSeq = %d, want >= 2 (usage-seq writes lost)", final.UsageSeq)
	}
}
