/**
 * Public surface of the shared API client. The actual fetch wrapper, the
 * domain-specific request methods, and their associated TypeScript types
 * live in per-domain modules under `@/shared/api/`. This file assembles
 * them into the single `apiClient` object the rest of the app imports
 * from `@/shared/api/api`.
 *
 * Why an aggregator instead of one giant file:
 *   - Each domain (auth, users, clients, servers, telemetry, jobs, …)
 *     has its own file with its own types and methods, keeping every
 *     module under ~300 lines.
 *   - Callers continue to use `import { apiClient, type Agent } from
 *     "@/shared/api/api"` exactly as before — no churn across the rest
 *     of the codebase.
 *   - When a new endpoint is added, only the relevant domain file plus
 *     the spread below need to change.
 */

export {
  ApiError,
  ApiSchemaError,
  API_SCHEMA_MISMATCH_EVENT,
  FORBIDDEN_EVENT,
  SESSION_EXPIRED_EVENT,
  api,
  apiBasePath,
  configuredRootPath,
} from "./http";
export type {
  ApiSchemaMismatchDetail,
  ForbiddenEventDetail,
} from "./http";

export type { MeResponse, TotpSetupResponse, TotpStatusResponse } from "./auth";
export type { CreateUserInput, LocalUser, UpdateUserInput } from "./users";
export type {
  AdoptDiscoveredClientResponse,
  Client,
  ClientDeployment,
  ClientIPEntry,
  ClientIPHistoryResponse,
  ClientInput,
  ClientListItem,
  DiscoveredClient,
  DiscoveredClientConflict,
} from "./clients";
export type {
  Agent,
  AgentCertificateRecovery,
  AgentRuntime,
  Instance,
  ProvisionOutboundAgentResponse,
  RuntimeEvent,
} from "./servers";
export type {
  ControlRoomResponse,
  DCHealthHistoryResponse,
  DCHealthPoint,
  FleetResponse,
  MetricSnapshot,
  ServerLoadHistoryResponse,
  ServerLoadPoint,
  TelemetryAgentLoadSeries,
  TelemetryAttentionItem,
  TelemetryDashboardResponse,
  TelemetryDetailBoost,
  TelemetryDiagnosticsRefreshResponse,
  TelemetryFreshness,
  TelemetryRecentEvent,
  TelemetryServerDetailResponse,
  TelemetryServerSummary,
  TelemetryServersResponse,
} from "./telemetry";
export type { AuditEvent, Job, JobCreateInput, JobTarget } from "./jobs";
export type {
  EnrollmentTokenListItem,
  EnrollmentTokenResponse,
  ScriptSource,
  ScriptSources,
} from "./enrollment";
export type {
  EnrollmentAttempt,
  EnrollmentAttemptDetail,
  EnrollmentEvent,
  EnrollmentLevel,
  EnrollmentMode,
  EnrollmentStatus,
} from "./types-enrollment";
export type {
  CreateFleetGroupRequest,
  CreateIntegrationProviderRequest,
  FleetGroupDeletionPreview,
  FleetGroupDeletionResult,
  FleetGroupEntry,
  FleetGroupIntegration,
  InstallFleetGroupIntegrationRequest,
  IntegrationKind,
  IntegrationProvider,
  IntegrationProviderKind,
  UpdateFleetGroupIntegrationRequest,
  UpdateFleetGroupRequest,
  UpdateIntegrationProviderRequest,
} from "./fleet-groups";
export type {
  AppearanceSettingsResponse,
  PanelSettingsResponse,
  RetentionSettings,
} from "./settings";
export type {
  UpdateSettings,
  UpdateSettingsResponse,
  UpdateState,
} from "./updates";
export type {
  CreateWebhookEndpointInput,
  UpdateWebhookEndpointInput,
  WebhookEndpoint,
  WebhookEndpointListResponse,
} from "./webhooks";

import { authApi } from "./auth";
import { clientsApi } from "./clients";
import { enrollmentApi } from "./enrollment";
import { fleetGroupsApi } from "./fleet-groups";
import { jobsApi } from "./jobs";
import { serversApi } from "./servers";
import { settingsApi } from "./settings";
import { telemetryApi } from "./telemetry";
import { updatesApi } from "./updates";
import { usersApi } from "./users";
import { webhooksApi } from "./webhooks";

export const apiClient = {
  ...authApi,
  ...telemetryApi,
  ...serversApi,
  ...usersApi,
  ...clientsApi,
  ...settingsApi,
  ...jobsApi,
  ...enrollmentApi,
  ...fleetGroupsApi,
  ...updatesApi,
  ...webhooksApi,
};

/*
 * =============================================================================
 * P2-FE-01 / BP-02 — Zod runtime validation migration status.
 * =============================================================================
 *
 * All response endpoints with a JSON body now flow through `parseWithSchema`,
 * and the bulk of the request payloads are validated via `encodeRequest`.
 *
 * Remaining unvalidated request payloads (all responses are validated):
 *
 *   - /settings/retention (PUT)            small admin-only shape
 *   - /settings/updates (PUT)              wide config blob, validated via
 *                                          updateSettingsRequestSchema in the
 *                                          form layer; left as raw POST here
 *                                          because the request type already
 *                                          mirrors the runtime type 1:1
 * Everything else is migrated as of BP-02 final tail.
 *
 * BP-02 S26 tail: the four /fleet-groups/{id}/integrations* endpoints
 * and the two /integration-providers (POST/PATCH) endpoints now go
 * through encodeRequest + a discriminated-union schema scoped to the
 * two most-likely kinds (webhook + dns-rr). Unknown kinds fall
 * through to a permissive branch since the server's IntegrationKind
 * registry is open. See
 * shared/api/schemas/requests/fleetGroupIntegrationRequest.ts for the
 * TODO(union) markers.
 */
