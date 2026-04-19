import { z } from "zod";

export const loginRequestSchema = z.object({
  username: z.string().min(1).max(256),
  password: z.string().min(1).max(1024),
  totp_code: z.string().regex(/^\d{6}$/).optional(),
});

export type LoginRequest = z.infer<typeof loginRequestSchema>;
