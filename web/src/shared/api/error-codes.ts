/**
 * Enumerated error codes returned by the control-plane HTTP API in the
 * `code` field of the standard error envelope. The list mirrors values
 * emitted by backend handlers (see internal/controlplane/server/*).
 *
 * Keeping this as a typed tuple (not an `enum`) lets us narrow
 * `ApiError.code` in container-level `onError` handlers via
 * `if (err.code === "invalid_credentials")` without importing runtime
 * enum objects.
 */
export const API_ERROR_CODES = [
  "invalid_credentials",
  "totp_required",
  "totp_invalid",
  "session_store_unavailable",
  "audit_persist_unavailable",
  "forbidden",
  "rate_limited",
  // Client-side sentinel: surfaced by api() when navigator.onLine is
  // false on a mutation. Not emitted by the backend.
  "offline",
] as const;

export type ApiErrorCode = (typeof API_ERROR_CODES)[number];

export function isApiErrorCode(value: unknown): value is ApiErrorCode {
  return typeof value === "string" && (API_ERROR_CODES as readonly string[]).includes(value);
}
