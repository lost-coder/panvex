import { z } from "zod";

export const panelUpdateRequestSchema = z.object({
  target_version: z.string().min(1).max(64),
});

export type PanelUpdateRequest = z.infer<typeof panelUpdateRequestSchema>;
