package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDisableTotpRaceIsBlocked verifies P2-SEC-08: two concurrent DisableTotp
// calls with the same TOTP code must not both succeed. The second caller
// must observe ErrInvalidTotpCode because the code was already consumed.
func TestDisableTotpRaceIsBlocked(t *testing.T) {
	now := time.Date(2026, time.April, 17, 11, 0, 0, 0, time.UTC)
	service := NewService()

	user, _, err := service.BootstrapUser(BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}

	secret, err := service.StartTotpSetup(user.ID, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("StartTotpSetup: %v", err)
	}
	enableCode, err := service.GenerateTotpCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode: %v", err)
	}
	if _, err := service.EnableTotp(user.ID, "Correct1horse2battery", enableCode, now.Add(30*time.Second)); err != nil {
		t.Fatalf("EnableTotp: %v", err)
	}

	// Use a distinct timestamp so the disable code differs from the one used
	// to enable — otherwise the enable already recorded it as consumed.
	disableAt := now.Add(2 * time.Minute)
	disableCode, err := service.GenerateTotpCode(secret, disableAt)
	if err != nil {
		t.Fatalf("GenerateTotpCode(disable): %v", err)
	}

	// Launch two goroutines racing to disable TOTP with the same code.
	var (
		wg        sync.WaitGroup
		successes atomic.Int32
		failures  atomic.Int32
		start     = make(chan struct{})
	)
	const attempts = 2
	errs := make([]error, attempts)
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			_, err := service.DisableTotp(user.ID, "Correct1horse2battery", disableCode, disableAt)
			errs[i] = err
			if err == nil {
				successes.Add(1)
			} else {
				failures.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("successful DisableTotp calls = %d, want exactly 1 (errs=%v)", got, errs)
	}
	if got := failures.Load(); got != 1 {
		t.Fatalf("failed DisableTotp calls = %d, want exactly 1 (errs=%v)", got, errs)
	}

	// The failing caller must see ErrInvalidTotpCode, not some other error.
	var sawInvalid bool
	for _, e := range errs {
		if e == ErrInvalidTotpCode {
			sawInvalid = true
			break
		}
	}
	if !sawInvalid {
		t.Fatalf("no caller observed ErrInvalidTotpCode; errs = %v", errs)
	}

	// Final state: TOTP is disabled on the user record.
	refreshed, err := service.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if refreshed.TotpEnabled {
		t.Fatal("TotpEnabled = true after race, want false")
	}
}
