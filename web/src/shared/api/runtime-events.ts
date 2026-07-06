import { api, apiBasePath, type RequestOpts } from "./http";
import { runtimeEventsListResponseSchema } from "./schemas/runtime-events";
import type {
  RuntimeEventLevel,
  RuntimeEventsListResponse,
} from "./types-runtime-events";

export interface RuntimeEventsFilter {
  levels?: RuntimeEventLevel[];
  limit?: number;
}

export const runtimeEventsApi = {
  // Phase-3 §3.x: fetch the recent runtime-event backlog for an agent.
  // The server returns the most recent N records (default 200, capped
  // at 500) ordered newest-first. The WebSocket `runtime.event` frames
  // then deliver new records in real-time — see useAgentRuntimeEvents
  // for the combined initial-list + live-stream hook.
  listRuntimeEvents: (agentId: string, filter?: RuntimeEventsFilter, opts?: RequestOpts) => {
    const params = new URLSearchParams();
    if (filter?.levels && filter.levels.length > 0) {
      params.set("level", filter.levels.join(","));
    }
    if (filter?.limit) {
      params.set("limit", String(filter.limit));
    }
    const qs = params.toString();
    return api<RuntimeEventsListResponse>(
      `${apiBasePath}/agents/${encodeURIComponent(agentId)}/runtime-events${qs ? "?" + qs : ""}`,
      { signal: opts?.signal },
      runtimeEventsListResponseSchema,
    );
  },
};
