import { z } from "zod";

import { id } from "./common.ts";

/**
 * GET /api/auth/me — identity payload bootstrapped by AuthProvider.
 *
 * role is kept as an open enum (z.enum) so that adding a new role on the
 * backend (e.g. "support") doesn't break every dashboard render — it
 * fails validation loudly, which is the desired behaviour for a security
 * primitive.
 */
export const meResponseSchema = z.object({
  id,
  username: z.string(),
  role: z.enum(["viewer", "operator", "admin"]),
  totp_enabled: z.boolean(),
});

/**
 * POST /api/auth/login — returns only a status string on success; the
 * session is set via HttpOnly cookie. Kept minimal intentionally.
 */
export const loginResponseSchema = z.object({
  status: z.string(),
});

export type MeParsed = z.infer<typeof meResponseSchema>;
export type LoginParsed = z.infer<typeof loginResponseSchema>;
