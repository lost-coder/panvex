// Feature-local React-Query key factory for the Settings → Webhooks
// admin surface. Mirrors the per-feature pattern used by users/,
// clients/, etc. — invalidations stay scoped so a webhook mutation
// doesn't ripple through unrelated caches.

export const webhooksKeys = {
  all: ["webhooks"] as const,
  list: () => [...webhooksKeys.all, "list"] as const,
};
