import { z } from "zod";

import type { components } from "../../openapi.gen.ts";

type Gen = components["schemas"];

export const updateAgentFleetGroupRequestSchema = z.object({
  fleet_group_id: z.string().min(1).max(64),
}) satisfies z.ZodType<Gen["UpdateAgentFleetGroupRequest"]>;

export type UpdateAgentFleetGroupRequest = z.infer<typeof updateAgentFleetGroupRequestSchema>;
