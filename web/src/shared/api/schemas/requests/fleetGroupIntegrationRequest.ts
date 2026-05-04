import { z } from "zod";

/**
 * R-Q-20 / BP-02 tail: Zod schemas for the four fleet-group
 * integration endpoints + the two integration-provider mutation
 * endpoints. The shared shape is `kind` + a free-form `config` blob;
 * the backend stores the blob as `json.RawMessage` and validates it
 * lazily via the per-kind `IntegrationKind.Validate` registered in
 * `internal/controlplane/fleet/integrations`.
 *
 * Because no concrete kinds are wired into the registry yet (the
 * registry is the Phase-3 plumbing only — see registry.go), the
 * frontend cannot enforce a single closed shape. We instead model
 * the most-likely two kinds as a discriminated union so the form
 * layer gets *some* compile-time safety, and fall through to a
 * permissive `unknown` branch for any kind the registry adds later.
 *
 * Scoped to:
 *   - kind: "webhook"   → URL + optional secret + optional headers
 *   - kind: "dns-rr"    → zone + record_name + ttl + optional weight
 *
 * TODO(union): when the registry grows additional kinds (e.g.
 * cloudflare-dns, smtp-notify, slack-notify, statuspage-publish),
 * add their config branches to `integrationConfigByKind` below
 * — keep them ordered alphabetically so reviews are mechanical.
 *
 * The `unknownIntegrationConfigSchema` branch matches *any* string
 * `kind` that is not in the closed set above, so unknown server
 * kinds round-trip through the client without breaking the form.
 * The server is the source of truth for validation in that case.
 */

// ---------------------------------------------------------------
// Per-kind config schemas. Keep these tight — they are the closed
// branches of the discriminated union.
// ---------------------------------------------------------------

const webhookConfigSchema = z.object({
  /** Target HTTPS endpoint that receives the JSON event payload. */
  url: z.string().url().max(2048),
  /** HMAC secret (optional) — the server signs each request body. */
  secret: z.string().max(512).optional(),
  /** Extra headers (optional) — used for tenant routing or auth. */
  headers: z.record(z.string(), z.string()).optional(),
});

const dnsRrConfigSchema = z.object({
  /** DNS zone the record lives under (no trailing dot). */
  zone: z.string().min(1).max(253),
  /** Record name relative to the zone (use "@" for apex). */
  record_name: z.string().min(1).max(253),
  /** TTL in seconds, bounded so misconfig cannot pin caches forever. */
  ttl: z.number().int().min(30).max(86_400),
  /** Optional weight for round-robin tie-breaks. */
  weight: z.number().int().min(0).max(1_000_000).optional(),
});

// ---------------------------------------------------------------
// Discriminated union over `kind`. The `unknown` branch is the
// fallback for any kind name not yet modelled here — server is the
// source of truth for those.
// ---------------------------------------------------------------

const knownIntegrationConfigSchema = z.discriminatedUnion("kind", [
  z.object({ kind: z.literal("webhook"), config: webhookConfigSchema }),
  z.object({ kind: z.literal("dns-rr"), config: dnsRrConfigSchema }),
]);

const knownKinds = ["webhook", "dns-rr"] as const;
type KnownKind = (typeof knownKinds)[number];

const unknownIntegrationConfigSchema = z.object({
  kind: z
    .string()
    .min(1)
    .max(64)
    .refine(
      (value): value is Exclude<string, KnownKind> =>
        !(knownKinds as readonly string[]).includes(value),
      { message: "kind is in the closed union, use that branch instead" },
    ),
  /**
   * Fallback config blob — passes through verbatim. The server
   * validates against its registry and 400s on mismatch. We accept
   * any JSON value here because the registry is open.
   */
  config: z.unknown(),
});

/**
 * `integrationKindWithConfigSchema` validates only the `(kind,
 * config)` pair. The four endpoints below extend it with their own
 * extra fields (`enabled`, `provider_id`, `label`).
 */
export const integrationKindWithConfigSchema = z.union([
  knownIntegrationConfigSchema,
  unknownIntegrationConfigSchema,
]);

export type IntegrationKindWithConfig = z.infer<typeof integrationKindWithConfigSchema>;

// ---------------------------------------------------------------
// /fleet-groups/{id}/integrations  POST  — install
// /fleet-groups/{id}/integrations/{integrationID}  PATCH — update
// ---------------------------------------------------------------

const fleetGroupIntegrationCommonShape = {
  /** Optional pointer to a shared provider (e.g. one Cloudflare
   *  account) — only allowed when the kind expects one. */
  provider_id: z.string().min(1).max(128).optional(),
  /** Whether the integration runs after install. */
  enabled: z.boolean(),
};

export const installFleetGroupIntegrationRequestSchema = z.union([
  z.object({
    kind: z.literal("webhook"),
    config: webhookConfigSchema,
    ...fleetGroupIntegrationCommonShape,
  }),
  z.object({
    kind: z.literal("dns-rr"),
    config: dnsRrConfigSchema,
    ...fleetGroupIntegrationCommonShape,
  }),
  z.object({
    kind: z
      .string()
      .min(1)
      .max(64)
      .refine(
        (value) => !(knownKinds as readonly string[]).includes(value),
        { message: "kind is in the closed union, use that branch instead" },
      ),
    config: z.unknown(),
    ...fleetGroupIntegrationCommonShape,
  }),
]);

export type InstallFleetGroupIntegrationRequestParsed = z.infer<
  typeof installFleetGroupIntegrationRequestSchema
>;

/**
 * The PATCH endpoint reuses the same shape minus `kind`, since the
 * kind is fixed at install time and supplied via the URL path.
 * Server still validates the config blob against the stored kind.
 */
export const updateFleetGroupIntegrationRequestSchema = z.object({
  provider_id: z.string().min(1).max(128).optional(),
  enabled: z.boolean(),
  /** Free-form because the form does not know which kind is being
   *  updated without an extra round-trip. The server resolves the
   *  stored kind and validates the config against it. */
  config: z.unknown(),
});

export type UpdateFleetGroupIntegrationRequestParsed = z.infer<
  typeof updateFleetGroupIntegrationRequestSchema
>;

// ---------------------------------------------------------------
// /integration-providers  POST + PATCH
// ---------------------------------------------------------------

/**
 * Shared-credentials provider — its config blob is validated by the
 * matching `ProviderKind.Validate` on the server. As with
 * integrations, the registry is open and unknown kinds pass through.
 *
 * TODO(union): when concrete provider kinds land (cloudflare-api,
 * smtp-relay, …), promote each to a closed branch here.
 */
export const createIntegrationProviderRequestSchema = z.object({
  kind: z.string().min(1).max(64),
  label: z.string().min(1).max(128),
  config: z.unknown(),
});

export type CreateIntegrationProviderRequestParsed = z.infer<
  typeof createIntegrationProviderRequestSchema
>;

export const updateIntegrationProviderRequestSchema = z.object({
  label: z.string().min(1).max(128),
  config: z.unknown(),
});

export type UpdateIntegrationProviderRequestParsed = z.infer<
  typeof updateIntegrationProviderRequestSchema
>;
