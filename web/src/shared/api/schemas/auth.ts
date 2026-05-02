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

/**
 * POST /api/auth/totp/setup — returns the freshly generated TOTP shared
 * secret + an otpauth:// URI the client renders into a QR code. Both
 * fields are bound to the current pending-setup record on the server;
 * a stale `setup` call invalidates the previous secret.
 */
export const totpSetupResponseSchema = z.object({
  secret: z.string().min(1),
  otpauth_url: z.string().min(1),
});

/**
 * POST /api/auth/totp/{enable,disable} — both endpoints return the
 * current TOTP-enabled state of the calling user. Schema is shared.
 */
export const totpStatusResponseSchema = z.object({
  totp_enabled: z.boolean(),
});

export type MeParsed = z.infer<typeof meResponseSchema>;
export type LoginParsed = z.infer<typeof loginResponseSchema>;
export type TotpSetupParsed = z.infer<typeof totpSetupResponseSchema>;
export type TotpStatusParsed = z.infer<typeof totpStatusResponseSchema>;
