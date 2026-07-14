// Package security mints and validates short-lived agent enrollment tokens.
//
// Why it stays its own package (R8-D reviewed it as a candidate for folding
// into a consumer): the only coherent home would be
// internal/controlplane/enrollment, which owns the enrollment domain — but that
// package is compiled into the AGENT binary too. Token MINTING is a panel-only
// capability, and R3 went to some trouble to keep the agent's dependency
// surface thin (it carries no storage layer, no DB driver, no control-plane
// server). Moving the minting code into a package the agent links would undo a
// little of that for no benefit, so it lives here: a leaf, panel-only, with one
// job.
package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"
)

var (
	// ErrEnrollmentTokenConsumed reports a replay attempt against a single-use token.
	ErrEnrollmentTokenConsumed = errors.New("enrollment token already consumed")
	// ErrEnrollmentTokenExpired reports a token that exceeded its validity window.
	ErrEnrollmentTokenExpired = errors.New("enrollment token expired")
	// ErrEnrollmentTokenInvalid reports an unknown or malformed enrollment token.
	ErrEnrollmentTokenInvalid = errors.New("enrollment token invalid")
	// ErrEnrollmentTokenTTLRequired reports a missing or non-positive TTL.
	ErrEnrollmentTokenTTLRequired = errors.New("enrollment token TTL must be positive")
)

// EnrollmentScope defines where a newly enrolled agent is allowed to attach.
type EnrollmentScope struct {
	FleetGroupID string
	TTL          time.Duration
}

// EnrollmentToken stores the minted token value and the scope bound to it.
type EnrollmentToken struct {
	Value        string
	FleetGroupID string
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

// MintEnrollmentToken produces one fresh token record for the given
// scope (S8). It is a pure function — all state lives in the storage
// layer now (see storage.Store.PutEnrollmentToken /
// ConsumeEnrollmentToken). The prior in-memory EnrollmentService has
// been removed because its data disappeared on restart and did not
// scale across multiple control-plane replicas, while every call-site
// already double-wrote into the persistent store.
func MintEnrollmentToken(scope EnrollmentScope, issuedAt time.Time) (EnrollmentToken, error) {
	if scope.TTL <= 0 {
		return EnrollmentToken{}, ErrEnrollmentTokenTTLRequired
	}

	value, err := randomToken(32)
	if err != nil {
		return EnrollmentToken{}, err
	}

	return EnrollmentToken{
		Value:        value,
		FleetGroupID: scope.FleetGroupID,
		IssuedAt:     issuedAt.UTC(),
		ExpiresAt:    issuedAt.UTC().Add(scope.TTL),
	}, nil
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
