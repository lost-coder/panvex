/**
 * Compile-time drift guard between the OpenAPI 3.1 source of truth
 * (openapi/panvex.yaml → openapi.gen.ts) and the hand-rolled Zod
 * schemas in `schemas/`.
 *
 * Wave 3.3 (see docs/superpowers/plans/2026-05-08-api-codegen.md)
 * keeps Zod for runtime parse — the schemas are intentionally
 * defensive (`agent.ts`: "schemas are DEFENSIVE, not prescriptive"),
 * so backend additions don't break the panel mid-flight. This file
 * shifts the *static* drift check to the type system: if the YAML
 * grows a required field that Zod doesn't parse, the build breaks
 * here instead of at first contact with a payload in production.
 *
 * Failure mode is "missing required field in Zod schema": fix by
 * adding the field to the matching schema, not by editing this file.
 *
 * Note: the check is intentionally one-directional and shallow
 * (top-level keys only). Zod is allowed to carry *extra* fields
 * (defensive parse). Nested objects are spot-checked separately
 * where they matter.
 */

import type { components } from "./openapi.gen.ts";
import type {
  AgentCertificateRecoveryParsed,
  AgentParsed,
} from "./schemas/agent.ts";
import type {
  EnrollmentTokenListItemParsed,
  EnrollmentTokenResponseParsed,
} from "./schemas/enrollment.ts";

type OpenAPISchemas = components["schemas"];

type RequiredKeys<T> = {
  [K in keyof T]-?: undefined extends T[K] ? never : K;
}[keyof T];

type MissingRequiredKeys<OpenAPI, Zod> = Exclude<
  RequiredKeys<OpenAPI>,
  keyof Zod
>;

/**
 * Resolves to literal `true` when every required key in `OpenAPI`
 * is present in `Zod`, otherwise to a tuple naming the offenders.
 * Assigning the named alias to a `true`-typed constant is what
 * actually triggers the build break.
 */
type AssertCoversRequiredKeys<OpenAPI, Zod> = [
  MissingRequiredKeys<OpenAPI, Zod>,
] extends [never]
  ? true
  : ["OpenAPI required fields missing from Zod schema:", MissingRequiredKeys<OpenAPI, Zod>];

// ─── Drift assertions ───────────────────────────────────────────────
//
// Each `_*Drift` const evaluates to `true` when the Zod schema covers
// the corresponding OpenAPI shape. A type error on any line below
// means the OpenAPI YAML and the Zod schema have drifted on a
// required top-level field.

const _agentDrift: AssertCoversRequiredKeys<
  OpenAPISchemas["Agent"],
  AgentParsed
> = true;

const _certificateRecoveryDrift: AssertCoversRequiredKeys<
  OpenAPISchemas["AgentCertificateRecoveryGrant"],
  // The embedded Zod shape (used inside `Agent.certificate_recovery`)
  // omits `agent_id` because the field is implicit from context.
  // Re-introduce it for the comparison so the guard accepts the OpenAPI
  // shape that always carries it.
  AgentCertificateRecoveryParsed & { agent_id: string }
> = true;

const _enrollmentListDrift: AssertCoversRequiredKeys<
  OpenAPISchemas["EnrollmentTokenListItem"],
  EnrollmentTokenListItemParsed
> = true;

const _enrollmentCreateDrift: AssertCoversRequiredKeys<
  OpenAPISchemas["CreateEnrollmentTokenResponse"],
  EnrollmentTokenResponseParsed
> = true;

// Keep the bindings live so tree-shaking doesn't drop the assertions.
export const __openapiDriftGuards = [
  _agentDrift,
  _certificateRecoveryDrift,
  _enrollmentListDrift,
  _enrollmentCreateDrift,
] as const;
