// @ts-nocheck
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import * as ts from "typescript";

function loadServerDetailPage(mocks) {
  const modulePath = fileURLToPath(new URL("./server-detail-page.tsx", import.meta.url));
  const source = readFileSync(modulePath, "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
      jsx: ts.JsxEmit.ReactJSX,
      esModuleInterop: true,
      allowSyntheticDefaultImports: true,
    },
    fileName: modulePath,
  }).outputText;

  const realRequire = createRequire(import.meta.url);
  const module = { exports: {} };
  const mockRequire = (specifier) => {
    if (specifier in mocks) {
      return mocks[specifier];
    }

    return realRequire(specifier);
  };

  const context = vm.createContext({
    require: mockRequire,
    module,
    exports: module.exports,
    __filename: modulePath,
    __dirname: path.dirname(modulePath),
    process,
    console,
  });

  new vm.Script(compiled, { filename: modulePath }).runInContext(context);

  return module.exports.ServerDetailPage;
}

function createViewModel() {
  return {
    header: {
      nameText: "de-fra-01",
      statusText: "Degraded",
      statusTone: "warn",
      groupText: "eu-edge",
      versionText: "1.14.7",
      lastSeenText: "24 Mar 2026, 11:58 UTC",
      readOnlyText: "Writable",
      certificateRecoveryText: "Not allowed",
      reasonText: "Runtime is degraded",
      freshnessText: "Fresh",
      detailBoostText: "Boost off",
    },
    overviewStats: [
      { label: "Active Users", valueText: "382", secondaryText: "Reported edge users" },
      { label: "Current Connections", valueText: "417", secondaryText: "341 ME, 76 direct" },
      { label: "DC Coverage", valueText: "83%", secondaryText: "10 of 12 DCs healthy" },
      { label: "Healthy Upstreams", valueText: "9 / 11", secondaryText: "2 degraded paths" },
      { label: "Accepting New Connections", valueText: "Yes", secondaryText: "Admission gates open" },
      { label: "Transport Mode", valueText: "Me Fallback", secondaryText: "Fallback still enabled" },
    ],
    runtimeProgressCards: [
      { label: "Startup Status", valueText: "Ready", secondaryText: "steady state", progressPct: 100 },
      { label: "Initialization", valueText: "Degraded", secondaryText: "waiting for repair paths", progressPct: 86 },
      { label: "Admission Gates", valueText: "Open", secondaryText: "new sessions allowed", progressPct: 100 },
    ],
    runtimeFlags: [
      { label: "Accepting New Connections", valueText: "Yes", secondaryText: "true" },
      { label: "ME Runtime Ready", valueText: "Yes", secondaryText: "true" },
      { label: "me2dc Fallback Enabled", valueText: "Yes", secondaryText: "true" },
      { label: "Use Middle Proxy", valueText: "No", secondaryText: "false" },
    ],
    dcRows: [
      {
        id: "dc--2",
        dcText: "-2",
        statusText: "Partial",
        statusTone: "warn",
        rttText: "2129 ms",
        coverageText: "33%",
        writersText: "2 / 3",
        endpointsText: "1 available",
        loadText: "0.93",
      },
    ],
    connectionStats: [
      { label: "Current Connections", valueText: "417", secondaryText: "Reported active sockets" },
      { label: "ME Connections", valueText: "341", secondaryText: "Reported through ME transport" },
      { label: "Direct Connections", valueText: "76", secondaryText: "Reported direct sessions" },
      { label: "Active Users", valueText: "382", secondaryText: "Reported unique users" },
    ],
    connectionMeta: [
      { label: "Connections Total", valueText: "21,833" },
      { label: "Bad Connections", valueText: "41" },
      { label: "Handshake Timeouts", valueText: "7" },
      { label: "Configured Users", valueText: "4,096" },
    ],
    upstreamSummaryText: "9 of 11 upstreams healthy",
    upstreamRows: [
      {
        id: "upstream-1",
        routeKindText: "fallback",
        addressText: "ams-relay-02:443",
        healthText: "Unhealthy",
        healthTone: "bad",
        failsText: "19",
        latencyText: "241 ms",
      },
    ],
    recentEventItems: [
      {
        id: "event-1",
        text: "DC 2 coverage dropped below quorum",
        time: "2 min ago",
        status: "bad",
      },
    ],
    diagnosticsRows: [
      { label: "Telemt Version", valueText: "2026.03" },
      { label: "Config Hash", valueText: "cfg-1" },
    ],
    securityRows: [
      { label: "API Read-Only", valueText: "No" },
      { label: "Whitelist Entries", valueText: "2" },
    ],
    meDiagnosticsRows: [
      { label: "Active Generation", valueText: "7" },
      { label: "Warm Generation", valueText: "8" },
    ],
    routingRows: [
      { label: "DC 2 Path", valueText: "149.154.167.40" },
    ],
    whitelistEntries: ["10.0.0.0/24", "192.168.0.0/24"],
    diagnosticsStateText: "Fresh",
    securityStateText: "Fresh",
    meDiagnosticsStateText: "Fresh",
  };
}

function createBaseMocks(overrides = {}) {
  return {
    "@tanstack/react-router": {
      useParams: () => ({ serverId: "agent-fra-01" }),
      useRouter: () => ({ history: { back: () => undefined } }),
    },
    "./server-detail.css": {},
    "./server-detail-hero": {
      ServerDetailHero: ({ header, onBack, onRecoveryAction, onDiagnosticsRefreshAction, recoveryActionLabel, diagnosticsRefreshActionLabel }) =>
        React.createElement(
          "section",
          {
            className: "server-detail-hero",
            "data-back-handler": typeof onBack === "function",
            "data-recovery-handler": typeof onRecoveryAction === "function",
            "data-diagnostics-refresh-handler": typeof onDiagnosticsRefreshAction === "function",
          },
          [
            "Back to Servers",
            header.nameText,
            header.statusText,
            "Group",
            header.groupText,
            "Version",
            header.versionText,
            "Last seen",
            header.lastSeenText,
            "Mode",
            header.readOnlyText,
            "Recovery",
            header.certificateRecoveryText,
            "Latest reported snapshot",
            diagnosticsRefreshActionLabel ?? "Diagnostics refresh unavailable",
            recoveryActionLabel ?? "Recovery action unavailable",
          ].join("|")
        ),
    },
    "./server-detail-kpis": {
      ServerDetailKpis: ({ stats }) =>
        React.createElement(
          "section",
          { className: "server-detail-kpis" },
          stats.map((stat) => `${stat.label}|${stat.valueText}|${stat.secondaryText}`).join("||")
        ),
    },
    "./server-detail-dc-table": {
      ServerDetailDcTable: ({ rows }) =>
        React.createElement(
          "section",
          { className: "server-detail-dc-table" },
          rows.map((row) => `${row.dcText}|${row.statusText}|${row.coverageText}`).join("||")
        ),
    },
    "./server-detail-runtime-panel": {
      ServerDetailRuntimePanel: ({ progressCards, flags }) =>
        React.createElement(
          "section",
          { className: "server-detail-runtime-panel" },
          [
            ...progressCards.map((card) => `${card.label}|${card.valueText}|${card.secondaryText}`),
            ...flags.map((flag) => `${flag.label}|${flag.valueText}|${flag.secondaryText}`),
          ].join("||")
        ),
    },
    "./server-detail-connections-panel": {
      ServerDetailConnectionsPanel: ({ stats, meta }) =>
        React.createElement(
          "section",
          { className: "server-detail-connections-panel" },
          [
            "Reported totals",
            ...stats.map((stat) => `${stat.label}|${stat.valueText}|${stat.secondaryText}`),
            ...meta.map((item) => `${item.label}|${item.valueText}`),
          ].join("||")
        ),
    },
    "./server-detail-upstreams-table": {
      ServerDetailUpstreamsTable: ({ summaryText, rows }) =>
        React.createElement(
          "section",
          { className: "server-detail-upstreams-table" },
          [
            summaryText,
            ...rows.map((row) => `${row.addressText}|${row.healthText}|${row.latencyText}`),
          ].join("||")
        ),
    },
    "./server-detail-events-panel": {
      ServerDetailEventsPanel: ({ items }) =>
        React.createElement(
          "section",
          { className: "server-detail-events-panel" },
          items.map((item) => `${item.text}|${item.time}`).join("||")
        ),
    },
    "./server-detail-diagnostics-panel": {
      ServerDetailDiagnosticsPanel: ({ stateText, rows }) =>
        React.createElement(
          "section",
          { className: "server-detail-diagnostics-panel" },
          [stateText, ...rows.map((row) => `${row.label}|${row.valueText}`)].join("||")
        ),
    },
    "./server-detail-security-panel": {
      ServerDetailSecurityPanel: ({ stateText, rows, entries }) =>
        React.createElement(
          "section",
          { className: "server-detail-security-panel" },
          [stateText, ...rows.map((row) => `${row.label}|${row.valueText}`), ...entries].join("||")
        ),
    },
    "./server-detail-me-diagnostics-panel": {
      ServerDetailMeDiagnosticsPanel: ({ stateText, meRows, routingRows }) =>
        React.createElement(
          "section",
          { className: "server-detail-me-diagnostics-panel" },
          [stateText, ...meRows.map((row) => `${row.label}|${row.valueText}`), ...routingRows.map((row) => `${row.label}|${row.valueText}`)].join("||")
        ),
    },
    "@/features/profile/profile-state": {
      useAppearanceSettings: () => ({ data: { help_mode: "basic" } }),
    },
    "./servers-state": {
        useServerDetail: () => ({
          data: {
            server: { agent: { id: "agent-fra-01", node_name: "de-fra-01" }, detail_boost: { active: false } },
            diagnostics: {},
            security_inventory: { entries: [] },
          },
          isLoading: false,
        }),
        useActivateTelemetryDetailBoost: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRefreshTelemetryDiagnostics: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useAllowAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRevokeAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useServerRecoveryAccess: () => ({
          canManageRecovery: true,
          canRefreshDiagnostics: true,
        }),
      },
    "./server-detail-view-model": {
      buildServerDetailViewModel: () => createViewModel(),
    },
    ...overrides,
  };
}

test("ServerDetailPage renders the approved first-slice detail layout", () => {
  const ServerDetailPage = loadServerDetailPage(createBaseMocks());

  const markup = renderToStaticMarkup(React.createElement(ServerDetailPage));

  assert.match(markup, /server-detail-page/);
  assert.match(markup, /server-detail-hero/);
  assert.match(markup, /server-detail-kpis/);
  assert.match(markup, /server-detail-page__secondary-grid/);
  assert.match(markup, /server-detail-page__tertiary-grid/);
  assert.match(markup, /server-detail-dc-table/);
  assert.match(markup, /server-detail-runtime-panel/);
  assert.match(markup, /server-detail-connections-panel/);
  assert.match(markup, /server-detail-upstreams-table/);
  assert.match(markup, /server-detail-events-panel/);
  assert.match(markup, /Back to Servers/);
  assert.match(markup, /Refresh Diagnostics/);
  assert.match(markup, /Allow Certificate Recovery/);
  assert.match(markup, /de-fra-01/);
  assert.match(markup, /Degraded/);
  assert.match(markup, /Group/);
  assert.match(markup, /Version/);
  assert.match(markup, /Last seen/);
  assert.match(markup, /Recovery/);
  assert.match(markup, /Active Users/);
  assert.match(markup, /Current Connections/);
  assert.match(markup, /DC Coverage/);
  assert.match(markup, /Healthy Upstreams/);
  assert.match(markup, /Runtime State/);
  assert.match(markup, /DC Health/);
  assert.match(markup, /Connections/);
  assert.match(markup, /Upstreams/);
  assert.match(markup, /Recent Events/);
  assert.match(markup, /System &amp; Limits/);
  assert.match(markup, /Security &amp; Whitelist/);
  assert.match(markup, /ME &amp; Routing Diagnostics/);
  assert.match(markup, /Latest reported snapshot/);
  assert.match(markup, /Reported totals/);
  assert.match(markup, /DC 2 coverage dropped below quorum\|2 min ago/);
  assert.match(markup, /ams-relay-02:443/);
  assert.match(markup, /Telemt Version\|2026.03/);
  assert.match(markup, /API Read-Only\|No/);
  assert.match(markup, /Active Generation\|7/);
  assert.match(markup, /DC 2 Path\|149\.154\.167\.40/);
  assert.match(
    markup,
    /server-detail-hero.*server-detail-kpis.*DC Health.*server-detail-page__secondary-grid.*Runtime State.*Connections.*server-detail-page__tertiary-grid.*Upstreams.*Recent Events.*server-detail-page__tertiary-grid.*System &amp; Limits.*Security &amp; Whitelist.*ME &amp; Routing Diagnostics/s
  );
});

test("ServerDetailPage renders a not-found state when the requested server is missing", () => {
  const ServerDetailPage = loadServerDetailPage(
    createBaseMocks({
      "@tanstack/react-router": {
        useParams: () => ({ serverId: "missing-agent" }),
        useRouter: () => ({ history: { back: () => undefined } }),
      },
      "./servers-state": {
        useServerDetail: () => ({
          data: undefined,
          isLoading: false,
        }),
        useActivateTelemetryDetailBoost: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRefreshTelemetryDiagnostics: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useAllowAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRevokeAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useServerRecoveryAccess: () => ({
          canManageRecovery: true,
          canRefreshDiagnostics: true,
        }),
      },
    })
  );

  const markup = renderToStaticMarkup(React.createElement(ServerDetailPage));

  assert.match(markup, /Server not found/i);
});

test("ServerDetailPage renders an error state when the servers query fails", () => {
  const ServerDetailPage = loadServerDetailPage(
    createBaseMocks({
      "./servers-state": {
        useServerDetail: () => ({
          data: undefined,
          isLoading: false,
          isError: true,
        }),
        useActivateTelemetryDetailBoost: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRefreshTelemetryDiagnostics: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useAllowAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRevokeAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useServerRecoveryAccess: () => ({
          canManageRecovery: true,
          canRefreshDiagnostics: true,
        }),
      },
    })
  );

  const markup = renderToStaticMarkup(React.createElement(ServerDetailPage));

  assert.match(markup, /Server data is unavailable/i);
});

test("ServerDetailPage hides the recovery action for non-admin users", () => {
  const ServerDetailPage = loadServerDetailPage(
    createBaseMocks({
      "./servers-state": {
        useServerDetail: () => ({
          data: {
            server: { agent: { id: "agent-fra-01", node_name: "de-fra-01" }, detail_boost: { active: false } },
            diagnostics: {},
            security_inventory: { entries: [] },
          },
          isLoading: false,
        }),
        useActivateTelemetryDetailBoost: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRefreshTelemetryDiagnostics: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useAllowAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRevokeAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useServerRecoveryAccess: () => ({
          canManageRecovery: false,
          canRefreshDiagnostics: false,
        }),
      },
    })
  );

  const markup = renderToStaticMarkup(React.createElement(ServerDetailPage));

  assert.doesNotMatch(markup, /Allow Certificate Recovery/);
  assert.match(markup, /Recovery action unavailable/);
});

test("ServerDetailPage renders a revoke action when a recovery window is already open", () => {
  const ServerDetailPage = loadServerDetailPage(
    createBaseMocks({
      "./servers-state": {
        useServerDetail: () => ({
          data: {
            server: {
              agent: {
                id: "agent-fra-01",
                node_name: "de-fra-01",
                certificate_recovery: {
                  status: "allowed",
                  issued_at_unix: 1711281600,
                  expires_at_unix: 1711282500,
                },
              },
              detail_boost: { active: false },
            },
            diagnostics: {},
            security_inventory: { entries: [] },
          },
          isLoading: false,
        }),
        useActivateTelemetryDetailBoost: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRefreshTelemetryDiagnostics: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useAllowAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useRevokeAgentCertificateRecovery: () => ({
          isPending: false,
          mutate: () => undefined,
        }),
        useServerRecoveryAccess: () => ({
          canManageRecovery: true,
          canRefreshDiagnostics: true,
        }),
      },
      "./server-detail-view-model": {
        buildServerDetailViewModel: () => ({
          ...createViewModel(),
          header: {
            ...createViewModel().header,
            certificateRecoveryText: "Allowed until 24 Mar 2026, 12:15 UTC",
          },
        }),
      },
    })
  );

  const markup = renderToStaticMarkup(React.createElement(ServerDetailPage));

  assert.match(markup, /Revoke Certificate Recovery/);
});
