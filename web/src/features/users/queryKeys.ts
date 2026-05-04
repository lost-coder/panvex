// BP-02: feature-local React-Query key factory for the users
// management surface (admin-only). See clients/queryKeys.ts for the
// rationale. Shape preserved verbatim from the pre-migration code.

export const usersKeys = {
  all: ["users"] as const,
  /** Unfiltered list — same shape as `all` (preserved verbatim). */
  list: () => [...usersKeys.all] as const,
};
