// BP-02: feature-local React-Query key factory for activity (jobs +
// audit). See clients/queryKeys.ts for the rationale. Shapes are
// preserved verbatim from the pre-migration code so cache identity
// stays stable for the WS-driven invalidation pipeline (event-
// invalidations.ts targets ["jobs"] and ["audit"]).

export const jobsKeys = {
  /** Root prefix — invalidate to flush every jobs query. */
  all: ["jobs"] as const,
  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...jobsKeys.all] as const,
};

export const auditKeys = {
  /** Root prefix — invalidate to flush every audit query. */
  all: ["audit"] as const,
  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...auditKeys.all] as const,
};
