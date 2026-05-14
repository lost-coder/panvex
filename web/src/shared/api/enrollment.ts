import { api, apiBasePath, encodeRequest } from "./http";
import {
  createEnrollmentTokenRequestSchema,
  enrollmentAttemptDetailSchema,
  enrollmentAttemptListResponseSchema,
  enrollmentTokenListSchema,
  enrollmentTokenResponseSchema,
} from "./schemas";
import type {
  EnrollmentAttempt,
  EnrollmentAttemptDetail,
  EnrollmentStatus,
} from "./types-enrollment";

// ScriptSource mirrors the OpenAPI ScriptSource — wire shape for one
// of the install-script source pointers the wizard renders. `sha256`
// is nullable rather than optional so callers can branch on
// `if (src.sha256 !== null)` rather than `if (src.sha256 !== undefined)`.
export type ScriptSource = {
  url: string;
  sha256: string | null;
};

export type ScriptSources = {
  panel: ScriptSource;
  github: ScriptSource;
};

export type EnrollmentTokenResponse = {
  value: string;
  panel_url: string;
  fleet_group_id: string;
  issued_at_unix: number;
  expires_at_unix: number;
  ca_pem: string;
  script_sources: ScriptSources;
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

  // Phase-1 observability: list recent enrollment attempts. The filter
  // arguments are all optional — the server defaults to the 20 most
  // recent attempts across the fleet when nothing is provided.
  listEnrollmentAttempts: (
    filter: {
      token_id?: string;
      agent_id?: string;
      status?: EnrollmentStatus;
      limit?: number;
    } = {},
  ) => {
    const params = new URLSearchParams();
    if (filter.token_id) params.set("token_id", filter.token_id);
    if (filter.agent_id) params.set("agent_id", filter.agent_id);
    if (filter.status) params.set("status", filter.status);
    if (filter.limit) params.set("limit", String(filter.limit));
    const qs = params.toString();
    const path = `${apiBasePath}/enrollment-attempts${qs ? "?" + qs : ""}`;
    return api<{ items: EnrollmentAttempt[] }>(
      path,
      undefined,
      enrollmentAttemptListResponseSchema,
    );
  },

  getEnrollmentAttempt: (id: string) =>
    api<EnrollmentAttemptDetail>(
      `${apiBasePath}/enrollment-attempts/${encodeURIComponent(id)}`,
      undefined,
      enrollmentAttemptDetailSchema,
    ),
};
