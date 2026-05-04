// BP-02: feature-local React-Query key factory.
//
// Why a factory: bare string-literal keys (`["clients"]`,
// `["client", id]`) scattered across hooks/containers made cross-
// feature invalidation a coordination problem — typo "clientz" or a
// mismatched shape silently broke cache eviction with no compile-time
// signal. The factory is the single source of truth for every clients
// query key. All consumers (hooks, containers, EventsSynchronizer)
// read keys from here; no string literals leak into call sites.
//
// Shapes preserved verbatim from the pre-migration code so this is a
// pure refactor: cache identity must not change or in-flight WS
// invalidations would miss the entries they were targeting.

export const clientsKeys = {
  /** Root prefix — invalidate to flush every clients-domain query. */
  all: ["clients"] as const,

  /**
   * Unfiltered list. Pre-migration this was identical to `all`; kept
   * that way so the cache shape does not change underneath
   * EventsSynchronizer's `["clients"]` broad sweep. When the feature
   * grows additional list variants, append a discriminator under
   * `[...all, "list", params]`.
   */
  list: () => [...clientsKeys.all] as const,

  /**
   * Per-client detail. Pre-migration shape was `["client", id]` (note:
   * singular, not under the "clients" root). Kept literal so existing
   * `EventsSynchronizer` predicate `query.queryKey[0] === "client"`
   * continues to match without code changes elsewhere.
   */
  detail: (id: string) => ["client", id] as const,

  /** Per-client IP history list, scoped under the detail key. */
  ipHistory: (id: string) => [...clientsKeys.detail(id), "ip-history"] as const,

  /** Discovered (unmanaged) clients pending adopt/ignore. */
  discovered: ["discovered-clients"] as const,
};
