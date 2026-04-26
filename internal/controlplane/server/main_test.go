package server

import (
	"os"
	"testing"
)

// TestMain disables the production /api/auth/login timing floor (R-S-19)
// for the entire server-package test binary. Without this, every login
// HTTP test pays a real 150ms wall-clock sleep and the suite balloons by
// minutes. Production cmd/control-plane never imports test code, so the
// override is scoped to test runs only.
func TestMain(m *testing.M) {
	loginTimingFloor = 0
	os.Exit(m.Run())
}
