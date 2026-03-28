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

function loadDashboardPage(mocks) {
  const modulePath = fileURLToPath(new URL("./dashboard-page.tsx", import.meta.url));
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

  return module.exports.DashboardPage;
}

function createDefaultMocks(overrides = {}) {
  return {
    "@/components/summary-stats-row": {
      SummaryStatsRow: ({ children }) =>
        React.createElement("section", { "data-slot": "summary-row" }, children),
    },
    "@/components/ui/stat-card": {
      StatCard: ({ label, value }) =>
        React.createElement("article", { "data-slot": "legacy-stat" }, `${label}|${value}`),
    },
    "./dashboard-summary-card": {
      DashboardSummaryCard: ({ label, value, secondaryText, breakdownItems }) =>
        React.createElement(
          "article",
          { "data-slot": "dashboard-summary-card", "data-label": label },
          [
            label,
            "|",
            String(value),
            "|",
            secondaryText ?? "",
            "|",
            (breakdownItems ?? [])
              .map((item) => `${item.label}:${item.value}`)
              .join(","),
          ].join("")
        ),
    },
    "./activity-panel": {
      ActivityPanel: () =>
        React.createElement("section", { "data-slot": "activity-panel" }, "activity-panel"),
    },
    "./dc-overview-panel": {
      DcOverviewPanel: () =>
        React.createElement("section", { "data-slot": "dc-overview-panel" }, "dc-overview-panel"),
    },
    "./server-card": {
      ServerCard: ({ item }) =>
        React.createElement(
          "article",
          { "data-slot": "server-card" },
          `server-card:${item.agent.id}`
        ),
    },
    "@/features/profile/profile-state": {
      useAppearanceSettings: () => ({ data: { help_mode: "basic" } }),
    },
    "./dashboard-state": {
      useDashboardData: () => ({
        data: {
          fleet: {
            total_agents: 12,
            online_agents: 8,
            degraded_agents: 3,
            offline_agents: 1,
            total_instances: 0,
            metric_snapshots: 0,
            live_connections: 19,
            accepting_new_connections_agents: 0,
            middle_proxy_agents: 0,
            dc_issue_agents: 0,
          },
          attention: [
            { agent_id: "server-a", node_name: "server-a", reason: "Telemetry is stale", severity: "warn", runtime_freshness: { state: "stale" } },
          ],
          server_cards: [
            { agent: { id: "server-a" }, runtime_freshness: { state: "fresh" }, detail_boost: { active: false } },
            { agent: { id: "server-b" }, runtime_freshness: { state: "fresh" }, detail_boost: { active: false } },
          ],
          runtime_distribution: { direct: 2 },
          recent_runtime_events: [],
        },
        isLoading: false,
        isError: false,
      }),
      useDashboardClients: () => ({
        data: [{ id: "client-a" }, { id: "client-b" }],
        isLoading: false,
        isError: false,
      }),
    },
    "./dashboard-view-model": {
      buildFleetKpiSummary: () => ({
        totalServers: 12,
        onlineServers: 8,
        degradedServers: 3,
        offlineServers: 1,
        totalClients: 45,
        activeConnections: 19,
        totalTrafficBytes: 1536,
        dcCoveragePct: 84,
      }),
      buildFleetDcCoverageSummary: () => ({
        totalDcCount: 12,
        okCount: 7,
        partialCount: 3,
        downCount: 2,
        rows: [],
      }),
    },
    ...overrides,
  };
}

test("DashboardPage renders the approved KPI row above the rest of the dashboard", () => {
  const DashboardPage = loadDashboardPage(createDefaultMocks());
  const markup = renderToStaticMarkup(React.createElement(DashboardPage));

  assert.match(markup, /Servers\|12\|\|Online:8,Degraded:3,Offline:1/);
  assert.match(markup, /Clients \/ Active Connections\|45\|19 active connections\|/);
  assert.match(markup, /Traffic\|1.5 KB\|\|/);
  assert.match(markup, /DC Coverage\|84%\|12 DCs tracked\|OK:7,Partial:3,Down:2/);
});

test("DashboardPage renders the server grid before the lower operational panels", () => {
  const DashboardPage = loadDashboardPage(createDefaultMocks());
  const markup = renderToStaticMarkup(React.createElement(DashboardPage));

  assert.match(
    markup,
    /Fleet Overview.*data-slot="summary-row".*Attention Queue.*server-card:server-a.*server-card:server-b.*Operational Context.*data-slot="activity-panel"/s
  );
  assert.match(
    markup,
    /data-slot="server-grid"[^>]*style="grid-template-columns:repeat\(auto-fill,\s*minmax\(340px,\s*1fr\)\)"/
  );
});

test("DashboardPage renders an inline error state when dashboard queries fail", () => {
  const DashboardPage = loadDashboardPage(
    createDefaultMocks({
      "./dashboard-state": {
        useDashboardData: () => ({ data: undefined, isLoading: false, isError: true }),
        useDashboardClients: () => ({ data: [], isLoading: false, isError: false }),
      },
    })
  );
  const markup = renderToStaticMarkup(React.createElement(DashboardPage));

  assert.match(markup, /Dashboard data is unavailable/i);
});
