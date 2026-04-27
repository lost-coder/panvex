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
	// RFC 5737 TEST-NET-1: documentation/test IPv4. Cannot route to real
	// hosts so it is safe to keep hard-coded inside contract fixtures.
	fixtureClientIPv4 = "192.0.2.1"
)
