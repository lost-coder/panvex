# ADR-008: Frontend 401 handling — global event + AuthProvider redirect

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-FE-02

## Context

When a session expired mid-session — cookie TTL reached, server-side
revocation via ADR-005, or a backend restart that invalidated
in-memory state — the dashboard kept rendering stale data. Fetches
returned 401, but each hook handled the response in its own ad-hoc
way: some showed an empty state, some showed a generic "error"
toast, most showed nothing. The user was stranded on a page that
silently refused to update and had to hit refresh manually. P2-FE-02
tracked this. We needed a single, predictable behaviour ("your
session has expired, we are taking you to login") driven from one
place.

## Decision

A two-layer contract:

1. **`api.ts` fetch wrapper** inspects every response. On `401`, it
   dispatches a `CustomEvent('panvex:session-expired')` on `window`
   and rejects the promise with a typed `SessionExpiredError`. This
   happens for every API call except the auth bootstrap endpoints
   (`/api/auth/me`, `/api/auth/login`, `/api/auth/logout`) — those
   are expected to return 401 when the user is not logged in and
   must not trigger the redirect.
2. **`AuthProvider`** listens for `panvex:session-expired` once at
   mount. On receipt it clears the TanStack Query cache
   (`queryClient.clear()`) to drop any rendered private data,
   shows a toast ("Your session has expired"), and navigates
   to `/login` with the current path stored as a `next=` query
   parameter so the user returns where they were.

## Alternatives considered

- **Per-hook `onError`.** Rejected: every feature that adds a new
  `useQuery` would need to remember to wire it, and we would never
  reach 100% coverage. The boilerplate cost was also unacceptable.
- **TanStack `QueryClient` global `onError`.** Considered, but
  ordering is unreliable: the error callback fires *after* the hook
  has already updated component state with an error, leading to a
  brief flash of error UI before the redirect. The event-based
  approach fires before React renders, so the redirect replaces the
  screen cleanly.
- **HTTP interceptor on fetch that redirects via
  `window.location.href`.** Rejected: bypasses the SPA router, loses
  toasts, and makes testing harder.

## Consequences

- Anywhere the app makes a request, a 401 is terminal and redirects.
  Code that wants to silently tolerate 401 (currently only the auth
  bootstrap endpoints) must use the escape hatch in `api.ts`.
- The `panvex:session-expired` event is part of the app contract.
  Any future micro-frontend or embedded widget must either listen
  for it or opt out of the `api.ts` wrapper.
