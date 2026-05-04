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
} from "./enrollment";
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
 *   - /fleet-groups/{id}/integrations*     four endpoints; medium-shaped
 *                                          payloads with a free-form `config`
 *                                          (json.RawMessage on the server). A
 *                                          single integrationConfig union schema
 *                                          would cover all four — defer to the
 *                                          integrations refactor.
 *   - /integration-providers (POST/PATCH)  same `config` blob constraint as
 *                                          above; moved with the same refactor.
 *
 * Everything else is migrated as of BP-02 final tail.
 */
