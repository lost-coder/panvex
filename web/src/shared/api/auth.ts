import { api, apiBasePath, encodeRequest } from "./http";
import {
  loginRequestSchema,
  loginResponseSchema,
  meResponseSchema,
  totpSetupResponseSchema,
  totpStatusResponseSchema,
  updateTotpRequestSchema,
} from "./schemas";
import type {
  LoginParsed,
  MeParsed,
  TotpSetupParsed,
  TotpStatusParsed,
} from "./schemas/auth";

export type MeResponse = MeParsed;
export type TotpSetupResponse = TotpSetupParsed;
export type TotpStatusResponse = TotpStatusParsed;

export const authApi = {
  login: (payload: { username: string; password: string; totp_code?: string }) =>
    api<LoginParsed>(`${apiBasePath}/auth/login`, {
      method: "POST",
      body: encodeRequest(`${apiBasePath}/auth/login`, loginRequestSchema, payload),
    }, loginResponseSchema),
  logout: () =>
    api<void>(`${apiBasePath}/auth/logout`, {
      method: "POST"
    }),
  me: () => api<MeResponse>(`${apiBasePath}/auth/me`, undefined, meResponseSchema),
  startTotpSetup: () =>
    api<TotpSetupResponse>(`${apiBasePath}/auth/totp/setup`, {
      method: "POST"
    }, totpSetupResponseSchema),
  enableTotp: (payload: { password: string; totp_code: string }) =>
    api<TotpStatusResponse>(`${apiBasePath}/auth/totp/enable`, {
      method: "POST",
      body: encodeRequest(
        `${apiBasePath}/auth/totp/enable`,
        updateTotpRequestSchema,
        payload,
      ),
    }, totpStatusResponseSchema),
  disableTotp: (payload: { password: string; totp_code: string }) =>
    api<TotpStatusResponse>(`${apiBasePath}/auth/totp/disable`, {
      method: "POST",
      body: encodeRequest(
        `${apiBasePath}/auth/totp/disable`,
        updateTotpRequestSchema,
        payload,
      ),
    }, totpStatusResponseSchema),
};
