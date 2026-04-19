import { z } from "zod";

export const updateTotpRequestSchema = z.object({
  password: z.string().min(1).max(1024),
  totp_code: z.string().regex(/^\d{6}$/),
});

export type UpdateTotpRequest = z.infer<typeof updateTotpRequestSchema>;
