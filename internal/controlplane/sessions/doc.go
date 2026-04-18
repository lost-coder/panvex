// Package sessions holds the control-plane's HTTP session/auth support
// primitives that are orthogonal to the core auth service
// (controlplane/auth).
//
// This is task P3-ARCH-01c of the god-package split (remediation plan v4).
// The package currently exports:
//
//   - LockoutTracker: per-username consecutive-failure tracker used by
//     the login handler to temporarily lock accounts after too many bad
//     password/TOTP attempts.
//   - RateLimiter: fixed-window rate limiter + RequestClientIPKey helper
//     used by the HTTP middleware chain to protect login, enrollment,
//     and sensitive-action endpoints.
//
// Only pure primitives live here. HTTP middleware + per-request keying
// (requestClientRateLimitKey, requestSessionRateLimitKey, withRateLimit)
// intentionally remains in controlplane/server for now because those
// helpers reach into *Server state (trusted-proxy CIDRs, session
// lookup, s.now(), writeError). A later ARCH pass can move them if a
// thinner transport seam is introduced.
package sessions
