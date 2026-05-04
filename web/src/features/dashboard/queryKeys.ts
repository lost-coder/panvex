// BP-02: feature-local React-Query key factory for the dashboard
// page. The dashboard reads `["telemetry", "dashboard"]` exclusively;
// canonical key lives on `telemetryKeys.dashboard()` in the servers
// feature so the WebSocket invalidation pipeline (event-invalidations
// + EventsSynchronizer + telemetry-query-invalidation) can target
// the same shape. Re-exporting here keeps the dashboard feature's
// import line short and signals which key it owns.

import { telemetryKeys } from "@/features/servers/queryKeys";

export const dashboardKeys = {
  /** Dashboard overview — alias of telemetryKeys.dashboard(). */
  overview: () => telemetryKeys.dashboard(),
};
