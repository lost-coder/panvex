import { api, apiBasePath, encodeRequest } from "./http";
import {
  createEnrollmentTokenRequestSchema,
  enrollmentAttemptDetailSchema,
  enrollmentAttemptListResponseSchema,
  enrollmentTokenListSchema,
  enrollmentTokenResponseSchema,
} from "./schemas";
import type {
  EnrollmentAttemptDetail,
  EnrollmentAttemptsFilter,
  EnrollmentAttemptsPage,
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
  // The listing endpoint masks the raw token (Q4.U-S-06): `value` is
  // omitted and the operator-safe `masked_value` + stable `handle` are
  // returned instead. `value` is therefore optional here — it is present
  // only on the creation response, never in listings.
  value?: string | undefined;
  masked_value?: string | undefined;
  handle?: string | undefined;
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
  // recent attempts across the fleet when nothing is provided. Phase-3
  // §3.b extended the filter with `mode`, `error_code`, started_*
  // bounds and an opaque `cursor` token so callers can paginate; the
  // response also gained `next_cursor` (null when there are no more
  // pages). Existing callers that only consume `.items` are
  // unaffected.
  listEnrollmentAttempts: (filter: EnrollmentAttemptsFilter = {}) => {
    const params = new URLSearchParams();
    if (filter.token_id) params.set("token_id", filter.token_id);
    if (filter.agent_id) params.set("agent_id", filter.agent_id);
    if (filter.status) params.set("status", filter.status);
    if (filter.mode) params.set("mode", filter.mode);
    if (filter.error_code) params.set("error_code", filter.error_code);
    if (filter.started_after) params.set("started_after", filter.started_after);
    if (filter.started_before) params.set("started_before", filter.started_before);
    if (filter.limit) params.set("limit", String(filter.limit));
    if (filter.cursor) params.set("cursor", filter.cursor);
    const qs = params.toString();
    const path = `${apiBasePath}/enrollment-attempts${qs ? "?" + qs : ""}`;
    return api<EnrollmentAttemptsPage>(
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
