// BP-02: feature-local React-Query key factory for auth.
//
// Audit BP-High flagged the bare `["me"]` singleton as a cache hazard
// — non-namespaced, vulnerable to silent collisions if any unrelated
// code ever shipped a literal `["me"]` for a different concept (e.g.
// per-user "me"-prefixed metrics). Move under an `auth` namespace so
// the key is self-describing and isolated.
//
// `userId` is optional and currently unused — the singleton fits one
// session at a time. The parameter exists so future per-user/multi-
// account flows (impersonation, account switcher) can scope without
// retrofitting every call site.

export const authKeys = {
  /** Auth feature root. */
  all: ["auth"] as const,

  /**
   * Profile of the currently authenticated user. Tuple shape lets a
   * future caller pass a userId for multi-account isolation; today
   * the backend cookie picks the user implicitly so callers omit it.
   */
  me: (userId?: string) =>
    userId === undefined
      ? (["auth", "me"] as const)
      : (["auth", "me", userId] as const),
};
