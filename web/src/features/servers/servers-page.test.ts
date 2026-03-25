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

function loadServersPage(mocks) {
  const modulePath = fileURLToPath(new URL("./servers-page.tsx", import.meta.url));
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

  return module.exports.ServersPage;
}

test("ServersPage renders template-style header, filters, view toggle, and shared table block", () => {
  const ServersPage = loadServersPage({
    "@tanstack/react-router": {
      useRouter: () => ({ navigate: () => undefined }),
    },
    "@/components/toolbar": {
      Toolbar: ({ search, filters, viewToggle }) =>
        React.createElement(
          "section",
          { "data-slot": "toolbar" },
          React.createElement("span", { "data-slot": "search-placeholder" }, search?.placeholder ?? ""),
          filters,
          viewToggle
        ),
    },
    "@/components/section-panel": {
      SectionPanel: ({ children }) =>
        React.createElement("div", { "data-slot": "section-panel" }, children),
    },
    "@/components/pagination": {
      Pagination: ({ page, totalPages }) =>
        React.createElement("div", { "data-slot": "pagination" }, `${page}/${totalPages}`),
    },
    "@/components/ui/filter-chip": {
      FilterChip: ({ label, count, active }) =>
        React.createElement("button", { "data-active": String(Boolean(active)) }, `${label}:${count ?? ""}`),
    },
    "@/components/ui/view-toggle": {
      ViewToggle: ({ current }) =>
        React.createElement("div", { "data-slot": "view-toggle" }, current),
    },
    "./server-table": {
      ServerTable: ({ rows, footer }) =>
        React.createElement(
          React.Fragment,
          null,
          React.createElement("div", { "data-slot": "server-table" }, `rows:${rows.length}`),
          footer
        ),
    },
    "./servers-state": {
      useServers: () => ({
        data: Array.from({ length: 9 }, (_, index) => ({
          id: `agent-${index + 1}`,
          node_name: `server-${index + 1}`,
          fleet_group_id: index % 2 === 0 ? "core" : "",
          presence_state: index === 0 ? "offline" : index === 1 ? "degraded" : "online",
          version: "1.2.3",
          read_only: false,
          last_seen_at: "2026-03-24T10:00:00Z",
          runtime: {
            degraded: index === 1,
            accepting_new_connections: index !== 0,
            active_users: index + 1,
            current_connections: index + 1,
            dc_coverage_pct: index === 0 ? 0 : 100,
            healthy_upstreams: index === 0 ? 0 : 2,
            total_upstreams: 2,
            dcs: [],
          },
        })),
        isLoading: false,
        isError: false,
      }),
    },
    "./servers-view-model": {
      buildServerFilterCounts: () => ({ all: 9, online: 7, issues: 2, offline: 1 }),
      buildServerTableRows: (agents) => agents.map((agent) => ({ id: agent.id, serverName: agent.node_name })),
      filterServerTableRows: (rows) => rows,
      paginateServerTableRows: (rows) => ({ rows: rows.slice(0, 8), totalPages: 2 }),
      sortServerTableRows: (rows) => rows,
    },
  });

  const markup = renderToStaticMarkup(React.createElement(ServersPage));

  assert.match(markup, /Servers/);
  assert.match(markup, /Manage MTProxy nodes/);
  assert.match(markup, /data-slot="search-placeholder">Search servers/);
  assert.match(markup, /All:9/);
  assert.match(markup, /Online:7/);
  assert.match(markup, /Issues:2/);
  assert.match(markup, /Offline:1/);
  assert.match(markup, /data-slot="view-toggle">table/);
  assert.match(markup, /data-slot="server-table">rows:8/);
  assert.match(markup, /data-slot="pagination">1\/2/);
});
