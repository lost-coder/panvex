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
      paths["/api/agents/{id}/config/apply"]["post"]["requestBody"]
    >["content"]["application/json"]
  >
>;

export type ApplyConfigRequest = z.infer<typeof applyConfigRequestSchema>;
