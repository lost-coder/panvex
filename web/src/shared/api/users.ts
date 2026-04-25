import { api, apiBasePath, encodeRequest } from "./http";
import {
  createUserRequestSchema,
  updateUserRequestSchema,
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
  users: () => api<LocalUser[]>(`${apiBasePath}/users`),
  createUser: (payload: CreateUserInput) =>
    api<LocalUser>(`${apiBasePath}/users`, {
      method: "POST",
      body: encodeRequest(`${apiBasePath}/users`, createUserRequestSchema, payload),
    }),
  updateUser: (userID: string, payload: UpdateUserInput) =>
    api<LocalUser>(`${apiBasePath}/users/${userID}`, {
      method: "PUT",
      body: encodeRequest(
        `${apiBasePath}/users/${userID}`,
        updateUserRequestSchema,
        payload,
      ),
    }),
  deleteUser: (userID: string) =>
    api<void>(`${apiBasePath}/users/${userID}`, {
      method: "DELETE"
    }),
  resetUserTotp: (userID: string) =>
    api<void>(`${apiBasePath}/users/${userID}/totp/reset`, {
      method: "POST"
    }),
};
