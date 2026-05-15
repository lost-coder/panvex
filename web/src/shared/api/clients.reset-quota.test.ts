// Reset-quota Phase 2 — API-client surface coverage.
//
// We exercise the two new endpoints through the public `apiClient`
// helpers so the test catches regressions in:
//   - the URL builder (per-agent variant must include {agent_id}),
//   - the zod parse path (response shape must include `client` + `job`,
//     missing-keys must surface as ApiSchemaError),
//   - the HTTP method / credentials wiring (POST + `credentials:
//     include` from the shared wrapper).
//
// The test mocks `globalThis.fetch` and seeds the CSRF cache so the
// wrapper does not try to fetch /api/auth/csrf-token before each
// mutation. This mirrors api.test.ts and keeps the suite hermetic.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiSchemaError } from "./http";
import { apiClient } from "./api";
import { __seedCSRFTokenForTesting } from "./http";

function makeJobShape(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "j-1",
    action: "reset_client_quota",
    target_agent_ids: ["a-1"],
    targets: [
      {
        agent_id: "a-1",
        status: "queued",
        result_text: "",
        result_json: "",
        updated_at: "2026-05-15T00:00:00Z",
      },
    ],
    ttl: 60_000_000_000,
    idempotency_key: "k1",
    actor_id: "u-1",
    status: "queued",
    payload_json: "{}",
    created_at: "2026-05-15T00:00:00Z",
    ...overrides,
  };
}

function makeClientShape(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "c-1",
    name: "alice",
    secret: "",
    user_ad_tag: "",
    enabled: true,
    traffic_used_bytes: 0,
    unique_ips_used: 0,
    active_tcp_conns: 0,
    max_tcp_conns: 0,
    max_unique_ips: 0,
    data_quota_bytes: 0,
    expiration_rfc3339: "",
    fleet_group_ids: [],
    agent_ids: [],
    deployments: [],
    created_at_unix: 0,
    updated_at_unix: 0,
    deleted_at_unix: 0,
    ...overrides,
  };
}

describe("apiClient.resetClientQuotaOnAgent", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("POSTs to /api/clients/{id}/reset-quota/{agent_id} and parses the response", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({ client: makeClientShape(), job: makeJobShape() }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    const result = await apiClient.resetClientQuotaOnAgent("c-1", "a-1");

    expect(result.client.id).toBe("c-1");
    expect(result.job.id).toBe("j-1");
    expect(result.job.targets).toHaveLength(1);
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/clients/c-1/reset-quota/a-1");
    expect(call[1]).toMatchObject({
      method: "POST",
      credentials: "include",
    });
  });

  it("throws ApiSchemaError when the response is missing the `job` envelope", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ client: makeClientShape() }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await expect(apiClient.resetClientQuotaOnAgent("c-1", "a-1")).rejects.toBeInstanceOf(
      ApiSchemaError,
    );
  });
});

describe("apiClient.resetClientQuotaFanOut", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("POSTs to /api/clients/{id}/reset-quota (no agent_id) and parses the response", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          client: makeClientShape({ id: "c-2" }),
          job: makeJobShape({
            id: "j-2",
            target_agent_ids: ["a-1", "a-2"],
            targets: [
              {
                agent_id: "a-1",
                status: "queued",
                result_text: "",
                result_json: "",
                updated_at: "2026-05-15T00:00:00Z",
              },
              {
                agent_id: "a-2",
                status: "queued",
                result_text: "",
                result_json: "",
                updated_at: "2026-05-15T00:00:00Z",
              },
            ],
          }),
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    const result = await apiClient.resetClientQuotaFanOut("c-2");

    expect(result.client.id).toBe("c-2");
    expect(result.job.id).toBe("j-2");
    expect(result.job.targets.map((t) => t.agent_id)).toEqual(["a-1", "a-2"]);
    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/clients/c-2/reset-quota");
    expect(call[1]).toMatchObject({ method: "POST" });
  });

  it("tolerates an empty `result_json` (default to '') on each target", async () => {
    // Defensive default in the jobs schema: backend may omit
    // result_json before the job has reached the agent. The wrapper
    // must accept this and zero it out so consumers don't have to
    // guard against `undefined`.
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          client: makeClientShape(),
          job: makeJobShape({
            targets: [
              {
                agent_id: "a-1",
                status: "queued",
                // result_text + result_json deliberately absent
                updated_at: "2026-05-15T00:00:00Z",
              },
            ],
          }),
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    const result = await apiClient.resetClientQuotaFanOut("c-1");
    expect(result.job.targets[0]?.result_json).toBe("");
    expect(result.job.targets[0]?.result_text).toBe("");
  });
});
