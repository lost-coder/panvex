package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestIsRetryableTxError (R4, audit §1.9): serialization_failure (40001) and
// deadlock_detected (40P01) are retryable; unrelated SQLSTATEs and non-pg
// errors are not.
func TestIsRetryableTxError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"serialization_failure", &pgconn.PgError{Code: "40001"}, true},
		{"deadlock_detected", &pgconn.PgError{Code: "40P01"}, true},
		{"wrapped_deadlock", fmt.Errorf("exec: %w", &pgconn.PgError{Code: "40P01"}), true},
		{"unique_violation", &pgconn.PgError{Code: "23505"}, false},
		{"check_violation", &pgconn.PgError{Code: "23514"}, false},
		{"plain_error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableTxError(tc.err); got != tc.want {
				t.Errorf("isRetryableTxError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
