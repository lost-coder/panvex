import { z } from "zod";

import type { components } from "../../openapi.gen.ts";

type Gen = components["schemas"];

/**
 * Body of `POST /api/agents/{id}/telemt/update` (Task 11). Both fields are
 * optional on the wire — an empty body dispatches the panel's cached latest
 * known Telemt release with no downgrade allowance. Mirrors
 * `agentUpdateRequestSchema`'s `.optional().default(...)` shape (needed so
 * Zod's output type, always-present, satisfies the OpenAPI-generated
 * optional-KEY type under exactOptionalPropertyTypes — see that schema's
 * comment) for the analogous agent-self-update dispatch, but this one
 * carries an extra `allow_downgrade` flag.
 */
export const dispatchTelemtUpdateRequestSchema = z.object({
  version: z.string().optional().default(""),
  allow_downgrade: z.boolean().optional().default(false),
}) satisfies z.ZodType<Gen["DispatchTelemtUpdateRequest"]>;

export type DispatchTelemtUpdateRequest = z.infer<typeof dispatchTelemtUpdateRequestSchema>;
