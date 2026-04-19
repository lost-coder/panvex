import { z } from "zod";

export const userRoleSchema = z.enum(["viewer", "operator", "admin"]);
export type UserRole = z.infer<typeof userRoleSchema>;

export const createUserRequestSchema = z.object({
  username: z.string().min(1).max(256),
  role: userRoleSchema,
  password: z.string().min(1).max(1024),
});

export type CreateUserRequest = z.infer<typeof createUserRequestSchema>;
