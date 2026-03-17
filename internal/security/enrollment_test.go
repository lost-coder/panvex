package security

import (
	"testing"
	"time"
)

func TestEnrollmentServiceConsumeRejectsExpiredToken(t *testing.T) {
	service := NewEnrollmentService()
	issuedAt := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(2 * time.Minute)

	token, err := service.IssueToken(EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          2 * time.Minute,
	}, issuedAt)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}

	_, err = service.ConsumeToken(token.Value, expiresAt.Add(time.Second))
	if err == nil {
		t.Fatal("ConsumeToken() error = nil, want expiration failure")
	}

	if err != ErrEnrollmentTokenExpired {
		t.Fatalf("ConsumeToken() error = %v, want %v", err, ErrEnrollmentTokenExpired)
	}
}

func TestEnrollmentServiceConsumePreservesScopeAndSingleUse(t *testing.T) {
	service := NewEnrollmentService()
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)

	token, err := service.IssueToken(EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          5 * time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}

	consumed, err := service.ConsumeToken(token.Value, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ConsumeToken() error = %v", err)
	}

	if consumed.FleetGroupID != "ams-1" {
		t.Fatalf("FleetGroupID = %q, want %q", consumed.FleetGroupID, "ams-1")
	}

	_, err = service.ConsumeToken(token.Value, now.Add(2*time.Minute))
	if err == nil {
		t.Fatal("ConsumeToken() second call error = nil, want single-use failure")
	}

	if err != ErrEnrollmentTokenConsumed {
		t.Fatalf("ConsumeToken() second call error = %v, want %v", err, ErrEnrollmentTokenConsumed)
	}
}
