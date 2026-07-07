/**
 * Drift guard between the OpenAPI 3.1 source of truth
 * (openapi/panvex.yaml → openapi.gen.ts) and the hand-rolled Zod
 * schemas in `schemas/`.
 *
 * Direction 1 — "every spec-required field is parsed, with the right
 * type" — is enforced AT THE SCHEMA DECLARATIONS via
 * `satisfies z.ZodType<components["schemas"][...]>` (see
 * schemas/agent.ts, schemas/enrollment.ts). That check is deep:
 * TypeScript assignability recurses through nested objects and arrays,
 * so this file no longer carries the old shallow `AssertCoversRequiredKeys`
 * machinery (P8.3 replaced it).
 *
 * Direction 2 — "every field a Zod schema parses is described in the
 * spec" — is what THIS file asserts. `satisfies` is covariant and lets
 * extra Zod fields through silently; historically that is exactly how
 * openapi/panvex.yaml lagged behind the backend (e.g.
 * connections_bad_by_class existed in Go and Zod but not in the YAML).
 * The check is per-level: each exported schema is asserted against its
 * own generated counterpart, so nesting is covered schema-by-schema.
 *
 * Failure mode reads as: `["Zod schema parses fields missing from the
 * OpenAPI spec:", "some_field"]` is not assignable to `true`. Fix by
 * adding the field to openapi/panvex.yaml (+ `make gen-openapi`) when
 * the backend really emits it, or by deleting it from the Zod schema
 * when it does not — never by editing this file's types.
 */

import type { components } from "./openapi.gen.ts";
import type {
  AgentCertificateRecoveryParsed,
  AgentParsed,
  AgentRuntimeParsed,
} from "./schemas/agent.ts";
import type {
  EnrollmentTokenListItemParsed,
  EnrollmentTokenResponseParsed,
} from "./schemas/enrollment.ts";

type OpenAPISchemas = components["schemas"];

type ExtraKeys<Zod, OpenAPI> = Exclude<keyof Zod, keyof OpenAPI>;

/**
 * Resolves to literal `true` when every key of the parsed Zod output
 * exists in the generated OpenAPI shape, otherwise to a tuple naming
 * the offenders. Assigning to a `true`-typed constant triggers the
 * build break.
 */
type AssertNoUnspecKeys<Zod, OpenAPI> = [ExtraKeys<Zod, OpenAPI>] extends [
  never,
]
  ? true
  : ["Zod schema parses fields missing from the OpenAPI spec:", ExtraKeys<Zod, OpenAPI>];

// ─── Direction-2 assertions ─────────────────────────────────────────

const _agentKeys: AssertNoUnspecKeys<AgentParsed, OpenAPISchemas["Agent"]> =
  true;

const _agentRuntimeKeys: AssertNoUnspecKeys<
  AgentRuntimeParsed,
  OpenAPISchemas["AgentRuntime"]
> = true;

const _certificateRecoveryKeys: AssertNoUnspecKeys<
  AgentCertificateRecoveryParsed,
  OpenAPISchemas["AgentCertificateRecoveryGrant"]
> = true;

const _enrollmentListKeys: AssertNoUnspecKeys<
  EnrollmentTokenListItemParsed,
  OpenAPISchemas["EnrollmentTokenListItem"]
> = true;

const _enrollmentCreateKeys: AssertNoUnspecKeys<
  EnrollmentTokenResponseParsed,
  OpenAPISchemas["CreateEnrollmentTokenResponse"]
> = true;

// Keep the bindings live so tree-shaking doesn't drop the assertions.
export const __openapiDriftGuards = [
  _agentKeys,
  _agentRuntimeKeys,
  _certificateRecoveryKeys,
  _enrollmentListKeys,
  _enrollmentCreateKeys,
] as const;
