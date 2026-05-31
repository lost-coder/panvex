import i18next from "i18next";
import type { ZodType } from "zod";

import { resolveAPIBasePath, resolveConfiguredRootPath } from "@/shared/lib/runtime-path";
import type { ApiErrorCode } from "./error-codes";

export const configuredRootPath = resolveConfiguredRootPath();
export const apiBasePath = resolveAPIBasePath(configuredRootPath);

// `(string & {})` keeps autocomplete for the known ApiErrorCode literals
// while still accepting an arbitrary string from the server (Sonar S6571
// otherwise flags `ApiErrorCode | string` as redundant).
type ApiErrorCodeOrString = ApiErrorCode | (string & {});

export class ApiError extends Error {
  code?: ApiErrorCodeOrString | undefined;
  constructor(message: string, code?: ApiErrorCodeOrString) {
    super(message);
    this.code = code;
  }
}

/**
 * Name of the CustomEvent dispatched on window when a response passes the
 * HTTP checks but fails our Zod schema (P2-FE-01 / DF-10). ToastProvider
 * (or any other boundary) can subscribe and surface a user-visible
 * toast; the raw ZodError is attached on `event.detail.error` for
 * debugging. We fire a DOM CustomEvent rather than importing ToastProvider
 * directly so that api.ts stays framework-free and testable in isolation.
 */
export const API_SCHEMA_MISMATCH_EVENT = "panvex:api-schema-mismatch";

export interface ApiSchemaMismatchDetail {
  path: string;
  error: unknown;
  message: string;
}

/**
 * Error thrown on schema validation failure. Separate from ApiError so
 * React Query can distinguish "server said 500" from "server said 200 but
 * the shape is wrong" — both are user-visible, but the latter is a bug
 * that needs to reach engineering not just the user.
 */
export class ApiSchemaError extends Error {
  readonly path: string;
  override readonly cause: unknown;
  constructor(path: string, cause: unknown) {
    super(`Response from ${path} did not match expected schema`);
    this.name = "ApiSchemaError";
    this.path = path;
    this.cause = cause;
  }
}

/**
 * Name of the CustomEvent dispatched on window when any authenticated
 * HTTP request returns 401 (P2-FE-02 / M-C12 / DF-12). AuthProvider
 * listens for this to clear the React Query cache and route to /login.
 *
 * Requests to /auth/login and /auth/me are excluded from dispatch to
 * avoid redirect loops (the login page already renders /login, and
 * /auth/me is the very check used to bootstrap the session — its 401
 * must surface normally so the router can decide to redirect).
 */
export const SESSION_EXPIRED_EVENT = "panvex:session-expired";

/**
 * Name of the CustomEvent dispatched when any authenticated request
 * returns 403 Forbidden. Semantically different from 401 — the user
 * is logged in but lacks permission for the attempted action. We fire
 * this once here so any UI boundary (AuthProvider / ToastProvider) can
 * show a single friendly message ("Недостаточно прав…") instead of a
 * generic backend message leaking through mutation.onError.
 *
 * We still throw ApiError for the caller, so container-level onError
 * can decide whether to add its own context; the global listener only
 * ensures the user gets a human-readable cue even if the caller forgot.
 */
export const FORBIDDEN_EVENT = "panvex:forbidden";

export interface ForbiddenEventDetail {
  path: string;
  /**
   * HTTP method that triggered the 403. Present so listeners can
   * debounce on `method:path` — otherwise a GET retry burst would
   * collapse with a PUT attempt that shares the same path.
   */
  method: string;
  message: string;
  code?: string | undefined;
}

/**
 * Name of the CustomEvent dispatched when a React Query mutation fails. The
 * UI's ToastProvider (or a future Sentry-bridge) listens for these to show a
 * single user-visible error and to forward the failure to a remote sink in
 * production. (BP-04 — replaces ad-hoc console.error in onError handlers.)
 *
 * In development we also call console.error for fast triage; in production
 * the console call is dropped (process.env.NODE_ENV check inside notifyMutationError)
 * so operator devtools stay clean.
 */
export const MUTATION_ERROR_EVENT = "panvex:mutation-error";

export interface MutationErrorDetail {
  /** Logical area: "auth", "servers", "clients", etc. */
  feature: string;
  /** Specific action — e.g. "totp.enable", "agent.rename". */
  action: string;
  /** The thrown error object — usually ApiError or schema parse error. */
  error: unknown;
}

/**
 * notifyMutationError is the canonical replacement for `console.error` in
 * React Query `onError` handlers. It dispatches MUTATION_ERROR_EVENT on
 * window so listeners (toast, Sentry-bridge) get a uniform structured event,
 * and in development still writes a console.error for live triage.
 *
 * Safe to call from non-window contexts (SSR, tests) — guarded.
 */
export function notifyMutationError(
  feature: string,
  action: string,
  error: unknown,
): void {
  if (process.env.NODE_ENV !== "production") {
    // Dev-only triage aid — drops out of production builds via
    // process.env.NODE_ENV constant folding.
    console.error(`[${feature}.${action}]`, error);
  }
  if (typeof globalThis.window === "undefined") return;
  const detail: MutationErrorDetail = { feature, action, error };
  globalThis.dispatchEvent(
    new CustomEvent<MutationErrorDetail>(MUTATION_ERROR_EVENT, { detail }),
  );
}

function isAuthBootstrapPath(path: string): boolean {
  // Strip any root-path prefix; we only care about the /api/... tail.
  // Match both `/api/auth/login` and `/api/auth/me` (with any query string).
  const apiIndex = path.indexOf("/api/");
  const tail = apiIndex >= 0 ? path.slice(apiIndex) : path;
  return (
    tail.startsWith("/api/auth/login") ||
    tail.startsWith("/api/auth/me")
  );
}

// Phase-2 §2.5: CSRF double-submit. The panel fetches a per-session
// token from /api/auth/csrf-token (sample once, cache, send on every
// mutation). On 403 we drop the cached value so the next mutation
// refetches — covers the panel-restart case where the server rotated
// its HMAC secret.
let csrfTokenPromise: Promise<string | null> | null = null;
let csrfToken: string | null = null;

function csrfTokenURL(): string {
  return `${apiBasePath}/auth/csrf-token`;
}

async function fetchCSRFToken(): Promise<string | null> {
  try {
    const response = await fetch(csrfTokenURL(), {
      method: "GET",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
    });
    if (!response.ok) {
      // Most likely 401 (no session yet). Caller treats null as "no
      // token available, omit the header" — the request will fail
      // downstream with the same 401, which the SESSION_EXPIRED
      // handler routes home.
      return null;
    }
    const payload = (await response.json()) as { token?: string };
    return typeof payload.token === "string" && payload.token !== ""
      ? payload.token
      : null;
  } catch {
    // Defensive: any network failure / mocked-fetch shape mismatch
    // here should NOT abort the actual mutation. Returning null sends
    // the mutation without the X-CSRF-Token header; the server will
    // reject it explicitly (403) which the global 403 handler then
    // surfaces. This keeps the wrapper resilient when called from
    // unit tests that mock fetch with a single Response.
    return null;
  }
}

async function ensureCSRFToken(): Promise<string | null> {
  if (csrfToken) return csrfToken;
  csrfTokenPromise ??= fetchCSRFToken().then((tok) => {
    csrfToken = tok;
    return tok;
  });
  try {
    return await csrfTokenPromise;
  } finally {
    csrfTokenPromise = null;
  }
}

function clearCSRFToken(): void {
  csrfToken = null;
  csrfTokenPromise = null;
}

// __seedCSRFTokenForTesting lets unit tests pre-populate the cache so
// the api() wrapper skips the GET /api/auth/csrf-token round-trip when
// fetch is mocked. Production code must NEVER call this — it is
// double-underscore-prefixed by intent.
export function __seedCSRFTokenForTesting(token: string | null): void {
  csrfToken = token;
  csrfTokenPromise = null;
}

function isMutationMethod(method: string): boolean {
  return method === "POST" || method === "PUT" || method === "PATCH" || method === "DELETE";
}

async function readErrorPayload(
  response: Response,
): Promise<{ message: string; code: string | undefined }> {
  let message = `Request failed with status ${response.status}`;
  let code: string | undefined;
  try {
    const payload = (await response.json()) as { error?: string; code?: string };
    if (payload.error) {
      message = payload.error;
    }
    code = payload.code;
  } catch {
    // Ignore JSON parsing failures for error responses.
  }
  return { message, code };
}

async function handleErrorResponse(
  response: Response,
  path: string,
  method: string,
  isMutation: boolean,
): Promise<never> {
  const isBootstrap = isAuthBootstrapPath(path);
  const hasWindow = globalThis.window !== undefined;

  // Global 401 interceptor (P2-FE-02 / M-C12): before this, idle users
  // whose server session had expired would see stale cached data plus a
  // cascade of red 401 errors instead of being routed to /login. Fire a
  // decoupled CustomEvent so AuthProvider (which owns router +
  // QueryClient access) can clear the cache and navigate. Skip the auth
  // bootstrap endpoints to avoid loops.
  if (response.status === 401 && hasWindow && !isBootstrap) {
    // Session is gone — drop any cached CSRF token so the next
    // post-login mutation refetches against the freshly-minted session.
    clearCSRFToken();
    globalThis.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT));
  }

  const { message, code } = await readErrorPayload(response);

  // Global 403 handler: surface a human-friendly cue so operators aren't
  // left staring at a terse "forbidden" toast. 403 on a state-changing
  // request can mean two things: legitimate role denial OR a stale CSRF
  // token (panel restarted, server secret rotated). Drop the cached
  // token so the next mutation refetches; the role-denial path is
  // unaffected because the new token will still fail.
  if (response.status === 403 && hasWindow && !isBootstrap) {
    if (isMutation) {
      clearCSRFToken();
    }
    const detail: ForbiddenEventDetail = { path, method, message, code };
    globalThis.dispatchEvent(
      new CustomEvent<ForbiddenEventDetail>(FORBIDDEN_EVENT, { detail }),
    );
  }

  throw new ApiError(message, code);
}

function parseWithSchema<T>(path: string, schema: ZodType<T>, json: unknown): T {
  const parsed = schema.safeParse(json);
  if (parsed.success) return parsed.data;

  // L-13: log structurally only in dev — production builds drop the
  // console.error so the schema-mismatch issues never surface in
  // operator devtools as a "noisy red error" while the dispatched
  // CustomEvent still reaches the UI's error boundary.
  if (process.env.NODE_ENV !== "production") {
    console.error("[api] schema mismatch", {
      path,
      issues: parsed.error.issues,
    });
  }

  if (globalThis.window !== undefined) {
    const detail: ApiSchemaMismatchDetail = {
      path,
      error: parsed.error,
      message: `Unexpected response shape from ${path}`,
    };
    globalThis.dispatchEvent(
      new CustomEvent<ApiSchemaMismatchDetail>(API_SCHEMA_MISMATCH_EVENT, {
        detail,
      }),
    );
  }

  throw new ApiSchemaError(path, parsed.error);
}

/**
 * Core HTTP helper. Three modes:
 *
 * 1. No schema — legacy call-site, cast to T (as before). Most endpoints
 *    still use this path; see the TODO block near the bottom of the
 *    apiClient object for the opt-in migration list.
 * 2. Schema provided — response JSON is fed through schema.parse(). On
 *    failure we:
 *      a. console.error the ZodError for dev visibility,
 *      b. dispatch `panvex:api-schema-mismatch` on window so any UI
 *         boundary (ToastProvider, sentry bridge, etc.) can surface it,
 *      c. throw an ApiSchemaError so React Query's isError surfaces.
 *    Importantly we DO NOT fall back to the raw payload — a mismatch
 *    means the UI was about to read a field it can't trust, and silent
 *    `undefined` propagation is the exact DF-10 failure mode we're
 *    eliminating.
 * 3. 204 No Content — schema skipped (there's no body), returns undefined.
 */
export async function api<T>(
  path: string,
  init?: RequestInit,
  schema?: ZodType<T>,
): Promise<T> {
  // Q3.U-Q-23: invariant on path. Every API call must traverse the
  // configured apiBasePath ("/api" or "<root>/api") so a future caller
  // typo cannot send credentialed requests to an attacker-controlled
  // host or to an unintended path on the same origin.
  if (!path.startsWith(apiBasePath + "/") && path !== apiBasePath) {
    throw new ApiError(
      `api(): path must start with ${apiBasePath}, got ${path}`,
      "invalid_path",
    );
  }

  // W15: fail mutations fast when the OS reports no network. Reads still
  // go through fetch so the browser cache / service worker can answer
  // them; mutations have nowhere to land, so surfacing "offline" here
  // saves the caller a 30s TCP timeout and preserves optimistic UIs.
  const method = (init?.method ?? "GET").toUpperCase();
  const isMutation = isMutationMethod(method);
  if (isMutation && navigator?.onLine === false) {
    // Data layer — no React context here, so resolve the string off the
    // i18next instance directly rather than via the useTranslation hook.
    throw new ApiError(i18next.t("ui:offline"), "offline");
  }

  // Phase-2 §2.5: attach the double-submit CSRF token on every state-
  // changing request that has a session. The login endpoint itself
  // can't carry a token (no session yet) so we skip the bootstrap path.
  const csrfHeaders: Record<string, string> = {};
  if (isMutation && !isAuthBootstrapPath(path)) {
    const token = await ensureCSRFToken();
    if (token) {
      csrfHeaders["X-CSRF-Token"] = token;
    }
  }

  const response = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...csrfHeaders,
      ...(init?.headers ?? {})
    },
    ...init
  });

  if (response.status === 204) {
    return undefined as T;
  }

  if (!response.ok) {
    await handleErrorResponse(response, path, method, isMutation);
  }

  const json = (await response.json()) as unknown;
  if (!schema) return json as T;
  return parseWithSchema(path, schema, json);
}

/**
 * Validate an outgoing request body against its Zod schema before it
 * leaves the client. Schema mismatches here indicate a frontend bug
 * (caller passed a shape the backend will reject), so we throw the same
 * ApiSchemaError used for unexpected responses. Tagging the path with
 * `(request)` lets the global listener distinguish request vs response
 * drift when triaging.
 */
export function encodeRequest<T>(path: string, schema: ZodType<T>, payload: unknown): string {
  const parsed = schema.safeParse(payload);
  if (!parsed.success) {
    // Q-9: match the response-side guard at L290 — keep dev visibility
    // for the Zod issue list but drop the noisy red error in production
    // builds. The thrown ApiSchemaError still propagates to callers.
    if (process.env.NODE_ENV !== "production") {
      console.error("[api] request schema mismatch", {
        path,
        issues: parsed.error.issues,
      });
    }
    throw new ApiSchemaError(`${path} (request)`, parsed.error);
  }
  return JSON.stringify(parsed.data);
}
