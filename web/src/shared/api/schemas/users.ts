import { z } from "zod";

import { id } from "./common.ts";

/**
 * R-Q-20: Zod schema for /users — admin-facing user inventory.
 */
export const userSchema = z.object({
  id,
  username: z.string(),
  // Open-coded enum so an unknown role is loud rather than silent.
  role: z.enum(["viewer", "operator", "admin"]),
  totp_enabled: z.boolean(),
});

export const userListSchema = z.array(userSchema);

export type UserParsed = z.infer<typeof userSchema>;
