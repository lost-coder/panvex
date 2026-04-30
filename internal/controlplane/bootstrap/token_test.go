package bootstrap

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestGenerateTokenHasExpectedSize(t *testing.T) {
	raw, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if len(raw) != tokenSize {
		t.Fatalf("token size: got %d, want %d", len(raw), tokenSize)
	}
}

func TestHashTokenIsStable(t *testing.T) {
	raw := []byte("deadbeefdeadbeefdeadbeefdeadbeef")
	expected := sha256.Sum256(raw)
	if got := hashToken(raw); got != expected {
		t.Fatalf("hashToken mismatch")
	}
}

func TestVerifyTokenAcceptsValidToken(t *testing.T) {
	now := time.Unix(2000, 0)
	issued, err := IssueToken(now, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	if err := VerifyToken(issued.Raw, issued.Hash, issued.ExpiresAt, now); err != nil {
		t.Fatalf("VerifyToken on fresh token: %v", err)
	}
}

func TestVerifyTokenRejectsExpired(t *testing.T) {
	now := time.Unix(2000, 0)
	issued, _ := IssueToken(now, time.Hour)
	err := VerifyToken(issued.Raw, issued.Hash, issued.ExpiresAt, now.Add(2*time.Hour))
	if err == nil {
		t.Fatal("expected expiration error")
	}
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestVerifyTokenRejectsWrongHash(t *testing.T) {
	now := time.Unix(2000, 0)
	issued, _ := IssueToken(now, time.Hour)
	var fakeHash [32]byte
	err := VerifyToken(issued.Raw, fakeHash, issued.ExpiresAt, now)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("expected ErrTokenMismatch, got %v", err)
	}
}

func TestVerifyTokenRejectsMalformedEncoding(t *testing.T) {
	now := time.Unix(2000, 0)
	issued, _ := IssueToken(now, time.Hour)
	err := VerifyToken("not!base64!", issued.Hash, issued.ExpiresAt, now)
	if err == nil {
		t.Fatal("expected encoding error")
	}
	if !errors.Is(err, ErrTokenInvalidShape) {
		t.Fatalf("expected ErrTokenInvalidShape, got %v", err)
	}
}
