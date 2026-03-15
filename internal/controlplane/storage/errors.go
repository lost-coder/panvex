package storage

import "errors"

var (
	// ErrNotFound reports a missing persisted record.
	ErrNotFound = errors.New("storage record not found")
	// ErrConflict reports a uniqueness or state-transition conflict during persistence.
	ErrConflict = errors.New("storage conflict")
)
