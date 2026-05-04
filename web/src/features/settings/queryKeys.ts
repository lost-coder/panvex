// BP-02: feature-local React-Query key factory for the settings
// surfaces (panel, appearance, retention, updates). See
// clients/queryKeys.ts for the rationale. Shapes preserved verbatim
// from the pre-migration code.

export const settingsKeys = {
  /** Root prefix — invalidate to flush every settings-domain query. */
  all: ["settings"] as const,

  /** Panel (HTTP/gRPC URL, password policy) settings. */
  panel: () => [...settingsKeys.all, "panel"] as const,

  /** Appearance (theme/density/help mode) settings. */
  appearance: () => [...settingsKeys.all, "appearance"] as const,

  /** Retention (audit/jobs TTL) settings. */
  retention: () => [...settingsKeys.all, "retention"] as const,

  /** GeoIP (city/ASN database) settings + runtime state. */
  geoip: () => [...settingsKeys.all, "geoip"] as const,
};
