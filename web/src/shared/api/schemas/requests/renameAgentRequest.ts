import { z } from "zod";

import type { components } from "../../openapi.gen.ts";

type Gen = components["schemas"];

export const renameAgentRequestSchema = z.object({
  node_name: z.string().min(1).max(256),
}) satisfies z.ZodType<Gen["RenameAgentRequest"]>;

export type RenameAgentRequest = z.infer<typeof renameAgentRequestSchema>;
