package storagetest

// Shared error-message format strings and fixture constants used across
// the runXContract test suites. Extracted to keep each call site to a
// single line and let Sonar S1192 stop firing on the test files.
const (
	errPutFleetGroupShort = "PutFleetGroup: %v"
	errPutFleetGroupLong  = "PutFleetGroup() error = %v"
	errPutAgentShort      = "PutAgent: %v"

	fixtureUserID            = "user-001"
	fixtureUserAppearanceKey = "user-appearance-default"
	fixtureClientIPv4        = "10.0.0.1"
)
