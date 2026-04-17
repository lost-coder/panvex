import { z } from "zod";

/**
 * GET /api/version — P1-SEC-15 discriminated response.
 *
 * Backend behaviour (internal/controlplane/server/http_updates.go):
 *   - Viewer role: { version }                           (no build fingerprint)
 *   - Operator+:   { version, commit_sha, build_time }   (full fingerprint)
 *
 * We model this as an `and` of a base schema plus an optional operator
 * block rather than a strict discriminatedUnion, because:
 *
 *   1. The discriminator (role) is not in the response body — it is on
 *      the session. A discriminated union would need a synthetic tag.
 *   2. The operator form is a strict superset of the viewer form; Zod's
 *      union with the fuller schema first gives the right precedence.
 *
 * Both branches are typed, so consumers can narrow with `"commit_sha" in x`.
 */

const viewerVersionSchema = z.object({
  version: z.string(),
});

const operatorVersionSchema = z.object({
  version: z.string(),
  commit_sha: z.string(),
  build_time: z.string(),
});

/**
 * Accept either shape. Try operator (strict, more specific) first so that
 * a payload with commit_sha is not silently downgraded to the viewer
 * branch via the less-specific match.
 */
export const versionSchema = z.union([operatorVersionSchema, viewerVersionSchema]);

export type VersionParsed = z.infer<typeof versionSchema>;
export type OperatorVersionParsed = z.infer<typeof operatorVersionSchema>;
export type ViewerVersionParsed = z.infer<typeof viewerVersionSchema>;

/** Narrowing helper — true for the Operator+ branch. */
export function isOperatorVersion(v: VersionParsed): v is OperatorVersionParsed {
  return "commit_sha" in v && typeof v.commit_sha === "string";
}
