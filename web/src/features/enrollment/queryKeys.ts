// BP-02: feature-local React-Query key factory for the enrollment
// tokens surface. See clients/queryKeys.ts for the rationale. Shape
// preserved verbatim from the pre-migration code.

export const enrollmentTokensKeys = {
  /** Root prefix — invalidate to flush every tokens query. */
  all: ["enrollmentTokens"] as const,
  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...enrollmentTokensKeys.all] as const,
};
