package security

import (
	"testing"
	"time"
)

func TestMintEnrollmentTokenRequiresPositiveTTL(t *testing.T) {
	_, err := MintEnrollmentToken(EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          0,
	}, time.Date(2026, time.April, 19, 8, 0, 0, 0, time.UTC))
	if err != ErrEnrollmentTokenTTLRequired {
		t.Fatalf("MintEnrollmentToken() error = %v, want %v", err, ErrEnrollmentTokenTTLRequired)
	}
}

func TestMintEnrollmentTokenPopulatesScope(t *testing.T) {
	issuedAt := time.Date(2026, time.April, 19, 8, 0, 0, 0, time.UTC)
	ttl := 5 * time.Minute

	token, err := MintEnrollmentToken(EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          ttl,
	}, issuedAt)
	if err != nil {
		t.Fatalf("MintEnrollmentToken() error = %v", err)
	}

	if token.FleetGroupID != "ams-1" {
		t.Fatalf("FleetGroupID = %q, want %q", token.FleetGroupID, "ams-1")
	}
	if token.IssuedAt != issuedAt.UTC() {
		t.Fatalf("IssuedAt = %v, want %v", token.IssuedAt, issuedAt.UTC())
	}
	if token.ExpiresAt.Sub(issuedAt.UTC()) != ttl {
		t.Fatalf("ExpiresAt - IssuedAt = %v, want %v", token.ExpiresAt.Sub(issuedAt.UTC()), ttl)
	}
	if token.Value == "" {
		t.Fatal("Value is empty")
	}
}

// Two mint calls must produce distinct random values even for the same
// scope and timestamp. Regression: a buggy refactor could seed the RNG
// from the clock and silently collide.
func TestMintEnrollmentTokenProducesUniqueValues(t *testing.T) {
	issuedAt := time.Date(2026, time.April, 19, 8, 0, 0, 0, time.UTC)
	a, err := MintEnrollmentToken(EnrollmentScope{FleetGroupID: "ams-1", TTL: time.Minute}, issuedAt)
	if err != nil {
		t.Fatalf("first mint: %v", err)
	}
	b, err := MintEnrollmentToken(EnrollmentScope{FleetGroupID: "ams-1", TTL: time.Minute}, issuedAt)
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if a.Value == b.Value {
		t.Fatal("two mints produced the same token value")
	}
}
