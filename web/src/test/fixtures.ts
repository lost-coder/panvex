// Shared test fixtures for the web suite.
//
// Today only `mockDirectServer` lives here — direct-relay layout tests
// need a `ServerDetailPageProps["server"]` shape with the direct-mode
// flags flipped and an upstream summary populated. The helper is
// generic enough to be reused by future tests; pass `overrides` to
// customise individual fields without rebuilding the whole fixture.
import type { ServerDetailPageProps } from "@/shared/api/types-pages/server-detail";

type ServerProps = ServerDetailPageProps["server"];

export interface MockDirectServerOverrides {
  fallback?: boolean;
  fallbackDurationSeconds?: number;
}

export function mockDirectServer(overrides: MockDirectServerOverrides = {}): ServerProps {
  const fallbackEnteredAtUnix =
    overrides.fallback === true
      ? Math.floor(Date.now() / 1000) - (overrides.fallbackDurationSeconds ?? 0)
      : null;

  return {
    id: "n-direct-1",
    name: "node-direct",
    status: "ok",
    systemInfo: {
      version: "1.2.3",
      targetArch: "x86_64",
      targetOs: "linux",
      buildProfile: "release",
      uptimeSeconds: 1234,
      configHash: "abc",
      configPath: "/etc/telemt.toml",
      configReloadCount: 0,
    },
    gates: {
      acceptingNewConnections: true,
      meRuntimeReady: false,
      useMiddleProxy: false,
      me2dcFallbackEnabled: false,
      rerouteActive: false,
      startupStatus: "ready",
      startupProgressPct: 100,
      degraded: false,
      readOnly: false,
    },
    dcs: [],
    connections: {
      current: 5,
      currentMe: 0,
      currentDirect: 5,
      activeUsers: 1,
      staleCacheUsed: false,
      topByConnections: [],
      topByThroughput: [],
    },
    summary: {
      connectionsTotal: 0,
      connectionsBadTotal: 0,
      handshakeTimeoutsTotal: 0,
      configuredUsers: 0,
    },
    upstreams: [
      {
        upstreamId: 1,
        routeKind: "direct",
        address: "1.2.3.4:443",
        weight: 1,
        healthy: true,
        fails: 0,
        lastCheckAgeSecs: 1,
        dc: [],
      },
      {
        upstreamId: 2,
        routeKind: "direct",
        address: "5.6.7.8:443",
        weight: 1,
        healthy: true,
        fails: 0,
        lastCheckAgeSecs: 1,
        dc: [],
      },
      {
        upstreamId: 3,
        routeKind: "direct",
        address: "9.10.11.12:443",
        weight: 1,
        healthy: true,
        fails: 0,
        lastCheckAgeSecs: 1,
        dc: [],
      },
    ],
    upstreamSummary: {
      configuredTotal: 3,
      healthyTotal: 3,
      unhealthyTotal: 0,
      directTotal: 3,
      socks4Total: 0,
      socks5Total: 0,
      shadowsocksTotal: 0,
      failRatePct5m: 0,
      failRateKnown: true,
      connectAttemptTotal: 0,
      connectSuccessTotal: 0,
      connectFailTotal: 0,
      connectFailfastTotal: 0,
    },
    events: [],
    eventsDroppedTotal: 0,
    useMiddleProxy: false,
    meRuntimeReady: false,
    me2dcFallbackEnabled: overrides.fallback === true,
    transportMode: "direct",
    fallbackEnteredAtUnix,
    telemtUnreachable: false,
    telemtUnreachableSinceUnix: 0,
  };
}
