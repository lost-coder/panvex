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
          presenceState: "online",
          isIssue: true,
          severity: 2,
          clientsValue: 342,
          clientsText: "342",
          cpuText: "—",
          memoryText: "—",
          trafficText: "—",
          uptimeText: "—",
          dcAvailableCount: 2,
          dcTotalCount: 3,
          dcSummaryText: "2/3",
          dcSegments: ["ok", "partial", "down"],
        },
      ],
      sortDir: "asc",
      sortKey: "server",
    })
  );

  assert.match(markup, /Server/);
  assert.match(markup, /Status/);
  assert.match(markup, /Clients/);
  assert.match(markup, /CPU/);
  assert.match(markup, /Memory/);
  assert.match(markup, /DC/);
  assert.match(markup, /Traffic/);
  assert.match(markup, /Uptime/);
  assert.match(markup, /Ungrouped/);
  assert.match(markup, /2\/3/);
  assert.match(markup, /data-slot="server-footer"/);
  assert.doesNotMatch(markup, /Actions/);
});
