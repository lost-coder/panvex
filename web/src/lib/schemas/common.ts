import { z } from "zod";

/**
 * Shared primitives reused across the schema package.
 *
 * Keeping these in one place means that when the backend changes its
 * conventions (e.g. switches from RFC3339 strings to unix integers) we
 * change exactly one location, not one per entity.
 */

/** Free-form non-empty ID — backend issues UUIDs today but we don't enforce
 * the UUID grammar so that any future ID scheme (e.g. KSUID) passes through
 * without a schema bump. */
export const id = z.string().min(1);

/** RFC3339 timestamp string; validated loosely as non-empty to survive
 * Go's time.Time zero-values ("0001-01-01T00:00:00Z") that some handlers
 * still emit on absent fields. */
export const timestamp = z.string();

/** Unix seconds (integer) — matches Go's time.Unix() / int64 pattern used
 * across the panel for monotonic counters. We accept 0 because many
 * "optional" timestamps serialize as 0 rather than being omitted. */
export const unixSeconds = z.number().int();

/**
 * Error envelope returned by the control-plane on non-2xx responses.
 * Mirrors `writeError` in internal/controlplane/server — error is the
 * human-readable message, code is the optional machine-readable tag.
 */
export const apiErrorSchema = z.object({
  error: z.string().optional(),
  code: z.string().optional(),
});

export type ApiErrorPayload = z.infer<typeof apiErrorSchema>;

/**
 * Empty 204 / ignored response body. Used by DELETE/POST endpoints that
 * return nothing meaningful — we still parse to catch the case where the
 * backend starts returning a payload we'd otherwise silently discard.
 */
export const emptyResponseSchema = z.void();
