import { api, apiBasePath, encodeRequest } from "./http";
import {
  createEnrollmentTokenRequestSchema,
  enrollmentTokenListSchema,
  enrollmentTokenResponseSchema,
} from "./schemas";

export type EnrollmentTokenResponse = {
  value: string;
  panel_url: string;
  fleet_group_id: string;
  issued_at_unix: number;
  expires_at_unix: number;
  ca_pem: string;
};

export type EnrollmentTokenListItem = {
  value: string;
  panel_url: string;
  fleet_group_id: string;
  status: "active" | "expired" | "consumed" | "revoked";
  issued_at_unix: number;
  expires_at_unix: number;
  // R-Q-20: `| undefined` widens the optional shape so Zod's
  // `.optional()` parser is type-compatible under
  // exactOptionalPropertyTypes.
  consumed_at_unix?: number | undefined;
  revoked_at_unix?: number | undefined;
};

export const enrollmentApi = {
  createEnrollmentToken: (payload: {
    fleet_group_id: string;
    ttl_seconds: number;
  }) =>
    api<EnrollmentTokenResponse>(
      `${apiBasePath}/agents/enrollment-tokens`,
      {
        method: "POST",
        body: encodeRequest(
          `${apiBasePath}/agents/enrollment-tokens`,
          createEnrollmentTokenRequestSchema,
          payload,
        ),
      },
      enrollmentTokenResponseSchema,
    ),
  listEnrollmentTokens: () =>
    api<EnrollmentTokenListItem[]>(
      `${apiBasePath}/agents/enrollment-tokens`,
      undefined,
      enrollmentTokenListSchema,
    ),
  revokeEnrollmentToken: (value: string) =>
    api<void>(`${apiBasePath}/agents/enrollment-tokens/${value}/revoke`, {
      method: "POST"
    }),
};
