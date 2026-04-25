import { api, apiBasePath, encodeRequest } from "./http";
import {
  loginRequestSchema,
  meResponseSchema,
  updateTotpRequestSchema,
} from "./schemas";

export type MeResponse = {
  id: string;
  username: string;
  role: string;
  totp_enabled: boolean;
};

export type TotpSetupResponse = {
  secret: string;
  otpauth_url: string;
};

export type TotpStatusResponse = {
  totp_enabled: boolean;
};

export const authApi = {
  login: (payload: { username: string; password: string; totp_code?: string }) =>
    api<{ status: string }>(`${apiBasePath}/auth/login`, {
      method: "POST",
      body: encodeRequest(`${apiBasePath}/auth/login`, loginRequestSchema, payload),
    }),
  logout: () =>
    api<void>(`${apiBasePath}/auth/logout`, {
      method: "POST"
    }),
  me: () => api<MeResponse>(`${apiBasePath}/auth/me`, undefined, meResponseSchema),
  startTotpSetup: () =>
    api<TotpSetupResponse>(`${apiBasePath}/auth/totp/setup`, {
      method: "POST"
    }),
  enableTotp: (payload: { password: string; totp_code: string }) =>
    api<TotpStatusResponse>(`${apiBasePath}/auth/totp/enable`, {
      method: "POST",
      body: encodeRequest(
        `${apiBasePath}/auth/totp/enable`,
        updateTotpRequestSchema,
        payload,
      ),
    }),
  disableTotp: (payload: { password: string; totp_code: string }) =>
    api<TotpStatusResponse>(`${apiBasePath}/auth/totp/disable`, {
      method: "POST",
      body: encodeRequest(
        `${apiBasePath}/auth/totp/disable`,
        updateTotpRequestSchema,
        payload,
      ),
    }),
};
