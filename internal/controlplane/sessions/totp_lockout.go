package sessions

import "time"

// TOTPLockoutMaxAttempts is the consecutive-failure threshold at which a
// TOTP-enabled account is temporarily locked out for the second factor
// (S-6). Stricter than LockoutMaxAttempts so an attacker who already
// guessed the password cannot indefinitely brute the 6-digit code at
// LockoutMaxAttempts/LockoutDuration (~480 codes/day) — see audit S-6.
const TOTPLockoutMaxAttempts = 3

// TOTPLockoutDuration is how long a TOTP-failed account stays locked
// after reaching TOTPLockoutMaxAttempts consecutive failures. Shorter
// than LockoutDuration because the legitimate user typically just needs
// to wait for the next 30 s code window after fat-fingering it.
const TOTPLockoutDuration = 5 * time.Minute

// TOTPLockoutTracker tracks consecutive failed TOTP-code attempts per
// username and locks the second factor independently of the password
// lockout (S-6). The two counters never share state — a wrong password
// does NOT bump the TOTP counter and vice versa — so an attacker who
// holds a valid password gets at most TOTPLockoutMaxAttempts code
// guesses per TOTPLockoutDuration window, not the LockoutMaxAttempts
// budget the password tracker grants.
//
// The counting mechanics come from counterLockout; this tracker adds only
// its thresholds. State is in-memory by design (no onPersist/onDelete
// hooks): a 6-digit TOTP code has a 30-90 s validity window and the user
// must produce a fresh code on retry, so preserving the failure counter
// across a control-plane restart adds no security value.
//
// All methods are safe for concurrent use.
type TOTPLockoutTracker struct {
	counterLockout
}

// NewTOTPLockoutTracker constructs a fresh, empty TOTP failure tracker
// with the production thresholds (3 attempts / 5 min).
func NewTOTPLockoutTracker() *TOTPLockoutTracker {
	return &TOTPLockoutTracker{
		counterLockout: newCounterLockout(TOTPLockoutMaxAttempts, TOTPLockoutDuration),
	}
}

// SetThresholds wires a live getter for the lockout window and fixes the
// max-attempts threshold. windowFn is called on every lockout evaluation
// so that operator changes to auth.totp_lockout_duration take effect
// without restarting the control-plane. maxAttempts is fixed at call time
// (TOTPLockoutMaxAttempts is not an audited tunable). Safe to call from
// any goroutine.
func (t *TOTPLockoutTracker) SetThresholds(maxAttempts int, windowFn func() time.Duration) {
	t.mu.Lock()
	t.maxAttempts = maxAttempts
	t.windowFn = windowFn
	t.mu.Unlock()
}
