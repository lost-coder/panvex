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

function makeApplyResultShape(
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    applied: 2,
    failed: "",
    error: "",
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

  it("POSTs to /api/agents/{id}/config/apply and parses the response", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify(makeApplyResultShape()), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.applyAgentConfig("a-1");

    expect(result.applied).toBe(2);
    expect(result.failed).toBe("");
    expect(result.error).toBe("");

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/agents/a-1/config/apply");
    expect(call[1]).toMatchObject({ method: "POST" });
  });

  it("applies defaults for missing applied/failed/error fields", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify({}), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const result = await configApi.applyAgentConfig("a-2");

    expect(result.applied).toBe(0);
    expect(result.failed).toBe("");
    expect(result.error).toBe("");
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
      new Response(
        JSON.stringify({
          batch_id: "cfgapply-42",
          jobs: [
            { agent_id: "a-1", job_id: "job-1" },
            { agent_id: "a-2", job_id: "" },
          ],
        }),
        {
          status: 202,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );

    const result = await configApi.applyGroupConfig("fg-1");

    expect(result.batch_id).toBe("cfgapply-42");
    expect(result.jobs).toHaveLength(2);
    // No-op agent carries an empty job id.
    expect(result.jobs[1]?.job_id).toBe("");

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/fleet-groups/fg-1/config/apply");
    expect(call[1]).toMatchObject({ method: "POST" });
  });
});

// ---------------------------------------------------------------------------
// GET /api/fleet-groups/{id}/config/apply/status
// ---------------------------------------------------------------------------

describe("configApi.groupConfigApplyStatus", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("builds paired agent/job query params and parses the aggregate", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          done: true,
          total: 2,
          applied: 1,
          failed: 1,
          pending: 0,
          agents: [
            { agent_id: "a-1", job_id: "job-1", status: "succeeded", message: "" },
            { agent_id: "a-2", job_id: "job-2", status: "failed", message: "boom" },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    const result = await configApi.groupConfigApplyStatus("fg-1", [
      { agent_id: "a-1", job_id: "job-1" },
      { agent_id: "a-2", job_id: "job-2" },
    ]);

    expect(result.done).toBe(true);
    expect(result.applied).toBe(1);
    expect(result.failed).toBe(1);

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    const url = String(call[0]);
    expect(url).toContain("/api/fleet-groups/fg-1/config/apply/status?");
    expect(url).toContain("agent=a-1");
    expect(url).toContain("job=job-1");
    expect(url).toContain("agent=a-2");
    expect(url).toContain("job=job-2");
  });
});
