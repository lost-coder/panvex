// Config API client surface coverage.
//
// Exercises every configApi method through the public surface so the
// test catches regressions in:
//   - URL builder (agent vs fleet-group variant),
//   - Zod parse path (response shapes, default values),
//   - HTTP method wiring (GET / PUT / POST),
//   - request body encoding (PUT sections payload).
//
// The test mocks `globalThis.fetch` and seeds the CSRF cache so the
// wrapper does not try to fetch /api/auth/csrf-token before each
// mutation. This mirrors clients.reset-quota.test.ts and keeps the
// suite hermetic.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { __seedCSRFTokenForTesting, ApiSchemaError } from "./http";
import { configApi } from "./config";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeAgentConfigShape(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    override: {},
    effective: { listen_port: 1080 },
    observed: { listen_port: 1080 },
    drift: { status: "in_sync", fields: [] },
    ...overrides,
  };
}

function makeGroupConfigShape(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    sections: { listen_port: 1080 },
    nodes: [{ agent_id: "a-1", status: "applied" }],
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// GET /api/agents/{id}/config
// ---------------------------------------------------------------------------

describe("configApi.getAgentConfig", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("GETs /api/agents/{id}/config and parses the response", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify(makeAgentConfigShape()), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.getAgentConfig("a-1");

    expect(result.drift.status).toBe("in_sync");
    expect(result.drift.fields).toEqual([]);
    expect(result.effective).toEqual({ listen_port: 1080 });

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/agents/a-1/config");
    expect(call[1]).toMatchObject({ credentials: "include" });
  });

  it("applies defaults for missing override / observed fields", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          // override and observed omitted — schema should default to {}
          effective: {},
          drift: { status: "drifted", fields: ["listen_port"] },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    const result = await configApi.getAgentConfig("a-2");

    expect(result.override).toEqual({});
    expect(result.observed).toEqual({});
    expect(result.drift.status).toBe("drifted");
    expect(result.drift.fields).toEqual(["listen_port"]);
  });

  it("throws ApiSchemaError when drift is missing", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({ override: {}, effective: {}, observed: {} }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    await expect(configApi.getAgentConfig("a-3")).rejects.toBeInstanceOf(ApiSchemaError);
  });
});

// ---------------------------------------------------------------------------
// PUT /api/agents/{id}/config
// ---------------------------------------------------------------------------

describe("configApi.putAgentConfig", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("PUTs to /api/agents/{id}/config with sections body", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await configApi.putAgentConfig("a-1", { listen_port: 2080 });

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/agents/a-1/config");
    expect(call[1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(call[1].body as string)).toEqual({
      sections: { listen_port: 2080 },
    });
  });
});

// ---------------------------------------------------------------------------
// POST /api/agents/{id}/config/apply
// ---------------------------------------------------------------------------

describe("configApi.applyAgentConfig", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("POSTs to /api/agents/{id}/config/apply and parses the 202 batch id", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ batch_id: "cfgapply-single-1" }), {
        status: 202,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.applyAgentConfig("a-1");

    // P3-3.4: single apply is now a persistent batch-of-one — 202 + batch_id.
    expect(result.batch_id).toBe("cfgapply-single-1");

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/agents/a-1/config/apply");
    expect(call[1]).toMatchObject({ method: "POST" });
  });
});

// ---------------------------------------------------------------------------
// GET /api/agents/{id}/config/apply/batches/{batchId}
// ---------------------------------------------------------------------------

describe("configApi.getAgentConfigApplyBatch", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("GETs the single-apply batch aggregate and parses the response", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          batch_id: "batch-1",
          mode: "all_at_once",
          status: "succeeded",
          done: true,
          total: 1,
          applied: 1,
          failed: 0,
          pending: 0,
          skipped: 0,
          agents: [
            { agent_id: "a-1", job_id: "job-1", status: "succeeded", message: "" },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    const result = await configApi.getAgentConfigApplyBatch("a-1", "batch-1");

    expect(result.batch_id).toBe("batch-1");
    expect(result.status).toBe("succeeded");
    expect(result.applied).toBe(1);

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/agents/a-1/config/apply/batches/batch-1");
  });
});

// ---------------------------------------------------------------------------
// GET /api/fleet-groups/{id}/config
// ---------------------------------------------------------------------------

describe("configApi.getGroupConfig", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("GETs /api/fleet-groups/{id}/config and parses the response", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify(makeGroupConfigShape()), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.getGroupConfig("fg-1");

    expect(result.sections).toEqual({ listen_port: 1080 });
    expect(result.nodes).toHaveLength(1);
    expect(result.nodes[0]?.agent_id).toBe("a-1");
    expect(result.nodes[0]?.status).toBe("applied");

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/fleet-groups/fg-1/config");
    expect(call[1]).toMatchObject({ credentials: "include" });
  });

  it("applies defaults for missing sections/nodes", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({}), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.getGroupConfig("fg-2");

    expect(result.sections).toEqual({});
    expect(result.nodes).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// PUT /api/fleet-groups/{id}/config
// ---------------------------------------------------------------------------

describe("configApi.putGroupConfig", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("PUTs to /api/fleet-groups/{id}/config with sections body", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await configApi.putGroupConfig("fg-1", { listen_port: 3000 });

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/fleet-groups/fg-1/config");
    expect(call[1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(call[1].body as string)).toEqual({
      sections: { listen_port: 3000 },
    });
  });
});

// ---------------------------------------------------------------------------
// POST /api/fleet-groups/{id}/config/apply
// ---------------------------------------------------------------------------

describe("configApi.applyGroupConfig", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("POSTs to /api/fleet-groups/{id}/config/apply and parses the 202 batch", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ batch_id: "cfgapply-42" }), {
        status: 202,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.applyGroupConfig("fg-1");

    // P3-3.4: the 202 body now carries only the batch id (per-job handles
    // were removed with the legacy job-id poller).
    expect(result.batch_id).toBe("cfgapply-42");

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/fleet-groups/fg-1/config/apply");
    expect(call[1]).toMatchObject({ method: "POST" });
  });
});

// ---------------------------------------------------------------------------
// GET /api/fleet-groups/{id}/config/apply/batches/{batchId}
// ---------------------------------------------------------------------------

describe("configApi.getGroupConfigApplyBatch", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("GETs the persistent-batch aggregate and parses the response, including skipped/halted", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          batch_id: "batch-1",
          mode: "rolling",
          status: "halted",
          done: true,
          total: 3,
          applied: 1,
          failed: 1,
          pending: 0,
          skipped: 1,
          agents: [
            { agent_id: "a-1", job_id: "job-1", status: "succeeded", message: "" },
            { agent_id: "a-2", job_id: "job-2", status: "failed", message: "disk full" },
            { agent_id: "a-3", job_id: "job-3", status: "skipped", message: "" },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    const result = await configApi.getGroupConfigApplyBatch("fg-1", "batch-1");

    expect(result.batch_id).toBe("batch-1");
    expect(result.status).toBe("halted");
    expect(result.skipped).toBe(1);
    expect(result.agents[1]?.message).toBe("disk full");
    expect(result.agents[2]?.status).toBe("skipped");

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/fleet-groups/fg-1/config/apply/batches/batch-1");
  });

  it("throws ApiSchemaError on an unrecognised batch status", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          batch_id: "batch-1",
          mode: "all_at_once",
          status: "bogus",
          agents: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    await expect(
      configApi.getGroupConfigApplyBatch("fg-1", "batch-1"),
    ).rejects.toBeInstanceOf(ApiSchemaError);
  });
});

// ---------------------------------------------------------------------------
// GET /api/fleet-groups/{id}/config/apply/batches?active=1
// ---------------------------------------------------------------------------

describe("configApi.activeGroupConfigApplyBatch", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("GETs the active-batch lookup and parses the running batch id", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({ batch_id: "batch-active-1" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.activeGroupConfigApplyBatch("fg-1");

    expect(result?.batch_id).toBe("batch-active-1");

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/fleet-groups/fg-1/config/apply/batches?active=1");
  });

  it("resolves undefined on 204 No Content (no batch in flight)", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );

    const result = await configApi.activeGroupConfigApplyBatch("fg-2");

    expect(result).toBeUndefined();
  });
});
