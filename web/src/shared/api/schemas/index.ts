/**
 * Barrel re-exports for the Zod schemas package.
 *
 * Why a barrel? api.ts imports from exactly one module instead of six,
 * and callers that want a specific schema (e.g. tests) can still reach
 * in via `lib/schemas/agent` if they need the unexported internals.
 */

export * from "./common.ts";
export * from "./agent.ts";
export * from "./client.ts";
export * from "./discovered.ts";
export * from "./dashboard.ts";
export * from "./auth.ts";
export * from "./version.ts";
// R-Q-20: response schemas for the activity / users / settings / fleet
// / enrollment endpoints — the UI imports these via apiClient parse calls.
export * from "./jobs.ts";
export * from "./users.ts";
export * from "./settings.ts";
export * from "./fleet.ts";
export * from "./enrollment.ts";
export * from "./requests/index.ts";
