// BP-02: feature-local React-Query key factory for the fleet-groups
// management surface (list, detail, deletion preview, integration
// kinds, integration providers). See clients/queryKeys.ts for the
// rationale. Shapes preserved verbatim from the pre-migration code so
// cache identity stays stable for the WS-driven invalidation pipeline
// — `fleet-groups` is also referenced by useServerMutations via
// `fleetGroupsKeys` from features/servers/queryKeys, so the root
// prefix is shared (and re-exported here so consumers can stay
// self-contained inside the feature folder).

import { fleetGroupsKeys as serversFleetGroupsKeys } from "@/features/servers/queryKeys";

export const fleetGroupsKeys = {
  /** Root prefix — invalidate when membership changes anywhere.
   *  Aliases the canonical root in features/servers/queryKeys to keep
   *  cache identity unchanged across the migration. */
  all: serversFleetGroupsKeys.all,

  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...serversFleetGroupsKeys.all] as const,

  /** Per-group detail (membership + integrations). */
  detail: (id: string | undefined) => ["fleet-group", id] as const,

  /** Pre-flight reassignment preview before DELETE. */
  deletionPreview: (id: string | undefined) =>
    ["fleet-group-deletion-preview", id] as const,
};

export const integrationKindsKeys = {
  /** Static-ish registry of supported integration kinds. */
  all: ["integration-kinds"] as const,
  list: () => ["integration-kinds"] as const,
};

export const integrationProviderKindsKeys = {
  /** Static-ish registry of supported provider kinds. */
  all: ["integration-provider-kinds"] as const,
  list: () => ["integration-provider-kinds"] as const,
};

export const integrationProvidersKeys = {
  /** Root prefix — invalidate to flush every providers query. */
  all: ["integration-providers"] as const,
  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...integrationProvidersKeys.all] as const,
  /** Per-provider detail. */
  detail: (id: string) => ["integration-provider", id] as const,
};
