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

function loadClientsPage(mocks) {
  const modulePath = fileURLToPath(new URL("./clients-page.tsx", import.meta.url));
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

  return module.exports.ClientsPage;
}

test("ClientsPage renders template-style header, filters, view toggle, and shared table block", () => {
  const ClientsPage = loadClientsPage({
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
    "@/components/pagination": {
      Pagination: ({ page, totalPages }) =>
        React.createElement("div", { "data-slot": "pagination" }, `${page}/${totalPages}`),
    },
    "@/components/data-table": {
      DataTable: ({ rows }) =>
        React.createElement("div", { "data-slot": "legacy-data-table" }, `rows:${rows.length}`),
    },
    "@/components/ui/filter-chip": {
      FilterChip: ({ label, count, active }) =>
        React.createElement("button", { "data-active": String(Boolean(active)) }, `${label}:${count ?? ""}`),
    },
    "@/components/ui/badge": {
      Badge: ({ children }) =>
        React.createElement("span", { "data-slot": "legacy-badge" }, children),
    },
    "@/components/ui/view-toggle": {
      ViewToggle: ({ current }) =>
        React.createElement("div", { "data-slot": "view-toggle" }, current),
    },
    "./client-table": {
      ClientTable: ({ rows, footer }) =>
        React.createElement(
          React.Fragment,
          null,
          React.createElement("div", { "data-slot": "client-table" }, `rows:${rows.length}`),
          footer
        ),
    },
    "./clients-state": {
      useClients: () => ({
        data: Array.from({ length: 9 }, (_, index) => ({
          id: `client-${index + 1}`,
          name: `Client ${index + 1}`,
          enabled: index !== 8,
          assigned_nodes_count: index % 4,
          expiration_rfc3339: "2026-04-03T12:00:00Z",
          traffic_used_bytes: index * 100,
          unique_ips_used: 1,
          active_tcp_conns: index < 6 ? index + 1 : 0,
          data_quota_bytes: 1000,
          last_deploy_status: index % 2 === 0 ? "success" : "pending_update",
        })),
        isLoading: false,
        isError: false,
      }),
    },
    "./clients-view-model": {
      buildClientFilterCounts: () => ({ all: 9, active: 6, idle: 2, disabled: 1 }),
      buildClientTableRows: (clients) => clients.map((client) => ({ id: client.id, clientName: client.name })),
      filterClientTableRows: (rows) => rows,
      paginateClientTableRows: (rows) => ({ rows: rows.slice(0, 8), totalPages: 2 }),
      sortClientTableRows: (rows) => rows,
    },
  });

  const markup = renderToStaticMarkup(React.createElement(ClientsPage));

  assert.match(markup, /Clients/);
  assert.match(markup, /Manage connected clients/);
  assert.match(markup, /data-slot="search-placeholder">Search clients/);
  assert.match(markup, /All:9/);
  assert.match(markup, /Active:6/);
  assert.match(markup, /Idle:2/);
  assert.match(markup, /Disabled:1/);
  assert.match(markup, /data-slot="view-toggle">table/);
  assert.match(markup, /data-slot="client-table">rows:8/);
  assert.match(markup, /data-slot="pagination">1\/2/);
});
