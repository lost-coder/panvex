// Phase 4d destination:
//   @/shared/api/api           — apiClient, ApiError, FORBIDDEN_EVENT, ...
//   @/shared/api/schemas/*     — Zod response schemas (barrel at ./schemas)
//   @/shared/api/schemas/requests/* — Phase 5 request-body Zod schemas
//   @/shared/api/transforms/*  — snake_case -> camelCase per domain
//
// Consumers import by explicit sub-path so the feature slice they
// belong to shows up in the diff. This index.ts is a deliberate no-op
// to keep the layering intent visible in the directory tree.
export {};
