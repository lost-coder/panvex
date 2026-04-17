# ADR-005: Session ID rotation on login + privilege change

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-SEC-01

## Context

The Phase 1 session middleware minted a session ID when an anonymous
visitor first touched the dashboard and reused the same ID after a
successful login. P2-SEC-01 called this out as a classic session
fixation vulnerability: an attacker who could plant a known session
cookie on a victim's browser (via a sibling subdomain, a shared
terminal, a phishing page, or XSS on an adjacent app) could then
observe the victim log in and ride the authenticated session. The
issue was compounded by the fact that changing a user's role or
password did nothing to their existing sessions — a user demoted from
admin to viewer kept admin-capable cookies alive until they expired
naturally.

## Decision

Two rotation rules, both enforced server-side:

1. **Rotate on login.** A successful `POST /api/auth/login`
   invalidates the pre-login session ID and issues a fresh one in
   the response `Set-Cookie`. The old ID is removed from the
   session store so any parallel holder loses access immediately.
2. **Revoke on privilege change.** Role change, password change,
   and user deletion all call
   `sessions.RevokeAllForUser(userID)`. Every active session for
   that user is deleted; the affected browsers receive a 401 on the
   next request and go through the login flow again.

Both operations emit audit events (`session.rotated`,
`session.revoked_all`) so the audit log reflects the cause.

## Alternatives considered

- **Never rotate.** Rejected: this was the status quo that produced
  the audit finding.
- **Rotate on every request.** Rejected: each response would carry a
  new `Set-Cookie`, racing with concurrent requests in the same
  browser tab and frequently dropping one of them. The UX impact was
  unacceptable and the added security is marginal once login
  rotation closes the fixation window.
- **Rotate on IP change / user-agent change.** Rejected: legitimate
  users roam between Wi-Fi and cellular, and UA strings change with
  browser updates. Too many false rotations to be useful.

## Consequences

- The session store must support deletion by user ID. Index added.
- Downstream tools (e.g. the CLI's "remember me" flow) must tolerate a
  session ID change across the login boundary. The test suite covers
  this.
- When we eventually ship remote logout ("sign out everywhere"),
  it reuses `RevokeAllForUser` — the mechanism is already in place.
