import { z } from "zod";

import { userRoleSchema } from "./createUserRequest";

export const updateUserRequestSchema = z.object({
  username: z.string().min(1).max(256),
  role: userRoleSchema,
  new_password: z.string().max(1024).optional(),
});

export type UpdateUserRequest = z.infer<typeof updateUserRequestSchema>;
