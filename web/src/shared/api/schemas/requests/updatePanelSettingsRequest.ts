import { z } from "zod";

export const updatePanelSettingsRequestSchema = z.object({
  http_public_url: z.string().max(512),
  grpc_public_endpoint: z.string().max(512),
  password_min_length: z.number().int().min(8).max(128),
});

export type UpdatePanelSettingsRequest = z.infer<typeof updatePanelSettingsRequestSchema>;
