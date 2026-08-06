import { z } from "zod";

import type { paths } from "@/shared/api/openapi.gen";

import { type LoosenOptional } from "../common.ts";

/**
 * Mirrors `reloadPolicyRequest` in
 * internal/controlplane/server/config_apply_reload.go — the optional
 * body accepted by both config-apply handlers (single agent and
 * fleet-group fan-out). An absent body (or all-empty fields) resolves
 * server-side to a 30s drain window; `instant` never carries a
 * timeout.
 *
 * `paths` (P2 Task 5/7) is agent-apply-only: the group fan-out handler
 * decodes the same Go struct but always discards it (restrictPaths is
 * passed as `nil` at the call site — see handleApplyGroupConfig), so
 * the OpenAPI spec only declares `paths` on the agent apply request
 * body. Sending it to the group endpoint would silently no-op, so this
 * schema is split in two to keep the contract honest: the base schema
 * below is bound to (and used for) the group endpoint, and
 * `agentApplyConfigRequestSchema` extends it with `paths` for the
 * agent endpoint.
 *
 * P8.3: the `satisfies` bind targets `LoosenOptional<...>` (see
 * common.ts) because openapi-typescript emits exact-optional fields
 * (`T?`) while Zod's `.optional()` types the output as `T | undefined`
 * — incompatible under this repo's `exactOptionalPropertyTypes`.
 */
export const applyConfigRequestSchema = z.object({
  reload_mode: z.enum(["instant", "drain"]).optional(),
  reload_timeout_secs: z.number().int().min(1).max(3600).optional(),
}) satisfies z.ZodType<
  LoosenOptional<
    NonNullable<
      paths["/api/fleet-groups/{id}/config/apply"]["post"]["requestBody"]
    >["content"]["application/json"]
  >
>;

export type ApplyConfigRequest = z.infer<typeof applyConfigRequestSchema>;

/**
 * Agent apply request body: the base reload policy plus the optional
 * `paths` restriction (P2 Task 5/7) — see the module doc above for why
 * this is not shared with the group endpoint.
 */
export const agentApplyConfigRequestSchema = z.object({
  reload_mode: z.enum(["instant", "drain"]).optional(),
  reload_timeout_secs: z.number().int().min(1).max(3600).optional(),
  paths: z.array(z.string()).optional(),
}) satisfies z.ZodType<
  LoosenOptional<
    NonNullable<
      paths["/api/agents/{id}/config/apply"]["post"]["requestBody"]
    >["content"]["application/json"]
  >
>;

export type AgentApplyConfigRequest = z.infer<typeof agentApplyConfigRequestSchema>;
