// @ts-nocheck
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import * as ts from "typescript";

async function loadServerTable() {
  const componentPath = fileURLToPath(new URL("./server-table.tsx", import.meta.url));
  const source = readFileSync(componentPath, "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
      jsx: ts.JsxEmit.ReactJSX,
      esModuleInterop: true,
      allowSyntheticDefaultImports: true,
    },
    fileName: componentPath,
  }).outputText;

  const realRequire = createRequire(import.meta.url);
  const module = { exports: {} as Record<string, unknown> };
  const mockRequire = (specifier: string) => {
    if (specifier === "@/components/data-table") {
      return {
        DataTable: ({ columns, footer, rows }) =>
          React.createElement(
            "section",
            { "data-slot": "data-table" },
            React.createElement(
              "div",
              { "data-slot": "headers" },
              columns.map((column: any) =>
                React.createElement("span", { key: column.key }, column.label)
              )
            ),
            rows.map((row: any) =>
              React.createElement(
                "article",
                { key: row.id, "data-row-id": row.id },
                columns.map((column: any) =>
                  React.createElement("div", { key: column.key, "data-col": column.key }, column.render(row))
                )
              )
            ),
            footer
          ),
      };
    }

    if (specifier === "lucide-react") {
      return {
        Server: (props: any) => React.createElement("svg", props),
      };
    }

    if (specifier === "./server-table.css") {
      return {};
    }

    if (specifier.includes("telemetry/help-metadata")) {
      return {
        getTelemetryFieldHelp: (label: string) => {
          const copy: Record<string, string> = {
            Health: "Primary operator diagnosis for why the node needs attention right now.",
            Freshness: "Telemetry freshness shows whether the latest runtime summary is still current enough for triage.",
            Upstreams: "Outbound route health summary for the node's current upstream set.",
          };
          return copy[label];
        },
      };
    }

    return realRequire(specifier);
  };

  const context = vm.createContext({
    require: mockRequire,
    module,
    exports: module.exports,
    __filename: componentPath,
    __dirname: path.dirname(componentPath),
    process,
    console,
  });

  new vm.Script(compiled, { filename: componentPath }).runInContext(context);

  return module.exports.ServerTable as (props: unknown) => React.ReactElement;
}

test("ServerTable renders agreed columns, placeholders, group text, dc summary, and footer", async () => {
  const ServerTable = await loadServerTable();
  const markup = renderToStaticMarkup(
    React.createElement(ServerTable, {
      footer: React.createElement("div", { "data-slot": "server-footer" }, "footer"),
      onSort: () => undefined,
      rows: [
        {
          id: "agent-1",
          serverName: "de-fra-01",
          groupText: "Ungrouped",
          statusText: "Degraded",
          statusTone: "degraded",
          reasonText: "Telemetry is stale",
          freshnessText: "Fresh",
          freshnessRank: 1,
          admissionText: "Open",
          runtimeText: "Direct • 342 conns",
          dcSummaryText: "2/3",
          upstreamSummaryText: "1 / 2 healthy",
          eventText: "No recent events",
          isIssue: true,
          severity: 2,
        },
      ],
      sortDir: "asc",
      sortKey: "server",
    })
  );

  assert.match(markup, /Server/);
  assert.match(markup, /Health/);
  assert.match(markup, /Freshness/);
  assert.match(markup, /Runtime/);
  assert.match(markup, /DC Health/);
  assert.match(markup, /Upstreams/);
  assert.match(markup, /Events/);
  assert.match(markup, /Ungrouped/);
  assert.match(markup, /2\/3/);
  assert.match(markup, /data-slot="server-footer"/);
  assert.doesNotMatch(markup, /Actions/);
});

test("ServerTable renders field help copy in full help mode", async () => {
  const ServerTable = await loadServerTable();
  const markup = renderToStaticMarkup(
    React.createElement(ServerTable, {
      footer: null,
      helpMode: "full",
      onSort: () => undefined,
      rows: [
        {
          id: "agent-2",
          serverName: "de-fra-02",
          groupText: "eu-edge",
          statusText: "Attention",
          statusTone: "degraded",
          reasonText: "Telemetry is stale",
          freshnessText: "Stale",
          freshnessRank: 2,
          admissionText: "Closed",
          runtimeText: "Direct • 12 conns",
          dcSummaryText: "73% across 4 DCs",
          upstreamSummaryText: "1 / 3 healthy",
          eventText: "DC 4 coverage dropped below quorum",
          isIssue: true,
          severity: 2,
        },
      ],
      sortDir: "asc",
      sortKey: "server",
    })
  );

  assert.match(markup, /Primary operator diagnosis for why the node needs attention right now\./);
  assert.match(markup, /Telemetry freshness shows whether the latest runtime summary is still current enough for triage\./);
  assert.match(markup, /Outbound route health summary for the node(?:&#x27;|')s current upstream set\./);
});
