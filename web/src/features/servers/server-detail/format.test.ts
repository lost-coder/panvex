import { describe, it, expect } from "vitest";

import type {
  ServerDcData,
  ServerDetailPageProps,
  ServerUpstreamSummaryData,
} from "@/shared/api/types-pages/pages";

import { computeAlertItems, statusSentence } from "./format";

function dc(
  num: number,
  coveragePct: number,
  alive: number,
  required: number,
): ServerDcData {
  return {
    dc: num,
    coveragePct,
    aliveWriters: alive,
    requiredWriters: required,
    rttMs: 25,
    load: 0,
  } as ServerDcData;
}

function gates(
  overrides: Partial<ServerDetailPageProps["server"]["gates"]> = {},
): ServerDetailPageProps["server"]["gates"] {
  return {
    acceptingNewConnections: true,
    meRuntimeReady: true,
    useMiddleProxy: true,
    me2dcFallbackEnabled: false,
    rerouteActive: false,
    startupStatus: "ready",
    startupProgressPct: 100,
    degraded: false,
    readOnly: false,
    ...overrides,
  };
}

function upstreamSummary(
  overrides: Partial<ServerUpstreamSummaryData> = {},
): ServerUpstreamSummaryData {
  return {
    configuredTotal: 3,
    healthyTotal: 3,
    unhealthyTotal: 0,
    directTotal: 3,
    socks4Total: 0,
    socks5Total: 0,
    shadowsocksTotal: 0,
    failRatePct5m: 0,
    failRateKnown: false,
    ...overrides,
  } as ServerUpstreamSummaryData;
}

describe("statusSentence — ME mode", () => {
  it("formats healthy with DC count", () => {
    expect(
      statusSentence("ok", { mode: "me", dcCount: 5, dcWarn: 0, dcErr: 0 }),
    ).toBe("HEALTHY · all 5 routes nominal");
  });

  it("singularizes one strained DC", () => {
    expect(
      statusSentence("warn", { mode: "me", dcCount: 5, dcWarn: 1, dcErr: 0 }),
    ).toBe("STRAINED · 1 DC under coverage");
  });

  it("pluralizes multiple offline DCs", () => {
    expect(
      statusSentence("error", { mode: "me", dcCount: 5, dcWarn: 0, dcErr: 3 }),
    ).toBe("DEGRADED · 3 DCs offline");
  });
});

describe("statusSentence — Direct mode (regression: no '0 DC under coverage')", () => {
  it("healthy reports upstream count, never DC count", () => {
    const s = statusSentence("ok", {
      mode: "direct",
      upstreamHealthy: 3,
      upstreamTotal: 3,
      failRatePct5m: 0,
      failRateKnown: true,
    });
    expect(s).toBe("HEALTHY · 3 upstreams nominal");
    expect(s).not.toContain("DC");
    expect(s).not.toContain("route");
  });

  it("warn for partial upstream health", () => {
    expect(
      statusSentence("warn", {
        mode: "direct",
        upstreamHealthy: 2,
        upstreamTotal: 3,
        failRatePct5m: 0,
        failRateKnown: false,
      }),
    ).toBe("STRAINED · 2/3 upstreams healthy");
  });

  it("warn surfaces fail-rate when above warn band", () => {
    expect(
      statusSentence("warn", {
        mode: "direct",
        upstreamHealthy: 3,
        upstreamTotal: 3,
        failRatePct5m: 17,
        failRateKnown: true,
      }),
    ).toBe("STRAINED · upstream fail-rate 17%");
  });

  it("warn handles no upstreams configured", () => {
    expect(
      statusSentence("warn", {
        mode: "direct",
        upstreamHealthy: 0,
        upstreamTotal: 0,
        failRatePct5m: 0,
        failRateKnown: false,
      }),
    ).toBe("STRAINED · no upstreams configured");
  });

  it("error reports all-upstreams-down", () => {
    expect(
      statusSentence("error", {
        mode: "direct",
        upstreamHealthy: 0,
        upstreamTotal: 3,
        failRatePct5m: 0,
        failRateKnown: false,
      }),
    ).toBe("DEGRADED · all upstreams down");
  });

  it("error surfaces high fail-rate", () => {
    expect(
      statusSentence("error", {
        mode: "direct",
        upstreamHealthy: 2,
        upstreamTotal: 3,
        failRatePct5m: 60,
        failRateKnown: true,
      }),
    ).toBe("DEGRADED · upstream fail-rate 60%");
  });

  it("never produces 'STRAINED · 0 DC under coverage' for healthy Direct nodes", () => {
    // The headline regression — backend used to label Direct as warn via
    // the ME-init Degraded flag, and the old statusSentence then rendered
    // "STRAINED · 0 DC under coverage" because dcs is empty. The Direct
    // branch must not invoke any DC vocabulary at all.
    for (const status of ["ok", "warn", "error"] as const) {
      const s = statusSentence(status, {
        mode: "direct",
        upstreamHealthy: 3,
        upstreamTotal: 3,
        failRatePct5m: 0,
        failRateKnown: true,
      });
      expect(s).not.toMatch(/\bDC\b/);
      expect(s).not.toMatch(/coverage/i);
    }
  });
});

describe("statusSentence — me_down", () => {
  it("ok renders standby copy", () => {
    expect(statusSentence("ok", { mode: "me_down" })).toBe(
      "STANDBY · ME pool initializing",
    );
  });

  it("error renders ME-pool-unavailable copy", () => {
    expect(statusSentence("error", { mode: "me_down" })).toBe(
      "DEGRADED · ME pool unavailable",
    );
  });
});

describe("computeAlertItems — ME mode", () => {
  it("collects DC coverage gaps", () => {
    const out = computeAlertItems({
      mode: "me",
      sortedDcs: [dc(1, 60, 3, 5), dc(2, 100, 5, 5)],
      gates: gates(),
      hasInitState: false,
    });
    expect(out).toEqual([
      {
        severity: "crit",
        message: "DC1 coverage at 60% (3/5 writers)",
        source: "dc-coverage",
      },
    ]);
  });

  it("prepends ME-runtime degraded alert when gates.degraded", () => {
    const out = computeAlertItems({
      mode: "me",
      sortedDcs: [],
      gates: gates({ degraded: true }),
      hasInitState: false,
    });
    expect(out[0]).toEqual({
      severity: "crit",
      message: "ME runtime is degraded",
      source: "gates",
    });
  });

  it("skips DC alerts during init", () => {
    const out = computeAlertItems({
      mode: "me",
      sortedDcs: [dc(1, 60, 3, 5)],
      gates: gates(),
      hasInitState: true,
    });
    expect(out).toEqual([]);
  });
});

describe("computeAlertItems — Direct mode (regression: no 'degraded mode' noise)", () => {
  it("does not emit 'ME runtime is degraded' even if gates.degraded slips through", () => {
    // Backend normalisation clears gates.degraded for Direct, but a stale
    // snapshot mid-deploy can carry the old value. The UI must not
    // surface the ME-flavoured alert under any circumstance for Direct.
    const out = computeAlertItems({
      mode: "direct",
      sortedDcs: [],
      gates: gates({ degraded: true }),
      hasInitState: false,
      upstreamSummary: upstreamSummary(),
    });
    expect(out.find((a) => a.source === "gates")).toBeUndefined();
  });

  it("surfaces fail-rate above the warn band", () => {
    const out = computeAlertItems({
      mode: "direct",
      sortedDcs: [],
      gates: gates(),
      hasInitState: false,
      upstreamSummary: upstreamSummary({
        failRatePct5m: 22,
        failRateKnown: true,
      }),
    });
    expect(out).toContainEqual({
      severity: "warn",
      message: "Upstream connect fail-rate at 22% (5m window)",
      source: "upstream-fail-rate",
    });
  });

  it("escalates fail-rate at 50%+", () => {
    const out = computeAlertItems({
      mode: "direct",
      sortedDcs: [],
      gates: gates(),
      hasInitState: false,
      upstreamSummary: upstreamSummary({
        failRatePct5m: 55,
        failRateKnown: true,
      }),
    });
    const first = out[0];
    expect(first).toBeDefined();
    expect(first?.severity).toBe("crit");
  });

  it("flags unhealthy upstream count", () => {
    const out = computeAlertItems({
      mode: "direct",
      sortedDcs: [],
      gates: gates(),
      hasInitState: false,
      upstreamSummary: upstreamSummary({
        configuredTotal: 3,
        healthyTotal: 1,
        unhealthyTotal: 2,
      }),
    });
    expect(out).toContainEqual({
      severity: "warn",
      message: "2/3 upstreams unhealthy",
      source: "upstream-health",
    });
  });

  it("emits 'no upstreams configured' when total is zero", () => {
    const out = computeAlertItems({
      mode: "direct",
      sortedDcs: [],
      gates: gates(),
      hasInitState: false,
      upstreamSummary: upstreamSummary({
        configuredTotal: 0,
        healthyTotal: 0,
        unhealthyTotal: 0,
      }),
    });
    expect(out).toContainEqual({
      severity: "warn",
      message: "No upstreams configured",
      source: "upstream-config",
    });
  });

  it("returns empty alert list for a healthy Direct node", () => {
    const out = computeAlertItems({
      mode: "direct",
      sortedDcs: [],
      gates: gates(),
      hasInitState: false,
      upstreamSummary: upstreamSummary(),
    });
    expect(out).toEqual([]);
  });
});
