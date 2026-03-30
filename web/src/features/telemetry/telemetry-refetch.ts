import type { TelemetryServerDetailResponse } from "../../lib/api";

const defaultTelemetryRefetchIntervalMs = 15_000;
const initializationWatchRefetchIntervalMs = 3_000;

export function telemetryServerDetailRefetchInterval(detail?: TelemetryServerDetailResponse): number {
  return detail?.initialization_watch?.visible ? initializationWatchRefetchIntervalMs : defaultTelemetryRefetchIntervalMs;
}
