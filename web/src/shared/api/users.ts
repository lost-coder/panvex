import { api, apiBasePath, encodeRequest, type RequestOpts } from "./http";
import {
  createUserRequestSchema,
  updateUserRequestSchema,
  userListSchema,
  userSchema,
} from "./schemas";

export type LocalUser = {
  id: string;
  username: string;
  role: string;
  totp_enabled: boolean;
  created_at?: string;
};

export type CreateUserInput = {
  username: string;
  role: string;
  password: string;
};

export type UpdateUserInput = {
  username: string;
  role: string;
  new_password?: string;
};

export const usersApi = {
  // R-Q-20: response Zod-parse on every read; mutations parse the
  // returned record with the same schema so the cache write is also
  // guarded.
  users: (opts?: RequestOpts) =>
    api<LocalUser[]>(`${apiBasePath}/users`, { signal: opts?.signal }, userListSchema),
  createUser: (payload: CreateUserInput) =>
    api<LocalUser>(
      `${apiBasePath}/users`,
      {
        method: "POST",
        body: encodeRequest(`${apiBasePath}/users`, createUserRequestSchema, payload),
      },
      userSchema,
    ),
  updateUser: (userID: string, payload: UpdateUserInput) =>
    api<LocalUser>(
      `${apiBasePath}/users/${userID}`,
      {
        method: "PUT",
        body: encodeRequest(
          `${apiBasePath}/users/${userID}`,
          updateUserRequestSchema,
          payload,
        ),
      },
      userSchema,
    ),
  deleteUser: (userID: string) =>
    api<void>(`${apiBasePath}/users/${userID}`, {
      method: "DELETE"
    }),
  resetUserTotp: (userID: string) =>
    api<void>(`${apiBasePath}/users/${userID}/totp/reset`, {
      method: "POST"
    }),
};
