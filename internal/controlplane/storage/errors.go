package storage

import "errors"

var (
	// ErrNotFound reports a missing persisted record.
	ErrNotFound = errors.New("storage record not found")
	// ErrConflict reports a uniqueness or state-transition conflict during persistence.
	ErrConflict = errors.New("storage conflict")
	// ErrNestedTransact is returned when a TxFn calls Transact on the tx
	// argument. Transactions are not reentrant — see P2-ARCH-01.
	ErrNestedTransact = errors.New("storage: nested Transact call not allowed")
)
