import { z } from "zod";

export const updatePanelSettingsRequestSchema = z.object({
  http_public_url: z.string().max(512),
  grpc_public_endpoint: z.string().max(512),
});

export type UpdatePanelSettingsRequest = z.infer<typeof updatePanelSettingsRequestSchema>;
