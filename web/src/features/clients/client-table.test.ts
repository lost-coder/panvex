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

async function loadClientTable() {
  const componentPath = fileURLToPath(new URL("./client-table.tsx", import.meta.url));
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

    if (specifier === "./client-table.css") {
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

  return module.exports.ClientTable as (props: unknown) => React.ReactElement;
}

test("ClientTable renders agreed columns, deploy status, quota, expires text, and footer", async () => {
  const ClientTable = await loadClientTable();
  const markup = renderToStaticMarkup(
    React.createElement(ClientTable, {
      footer: React.createElement("div", { "data-slot": "client-footer" }, "footer"),
      onSort: () => undefined,
      rows: [
        {
          id: "client-1",
          clientName: "Acme Alpha",
          deployStatusText: "Pending Update",
          statusText: "Idle",
          statusTone: "idle",
          statusRank: 2,
          connectionsValue: 0,
          connectionsText: "0",
          serversValue: 3,
          serversText: "3",
          trafficValue: 1610612736,
          trafficText: "1.5 GB",
          quotaValue: 5368709120,
          quotaText: "1.5 GB / 5.0 GB",
          expiresTimestamp: Date.UTC(2026, 3, 3, 12, 0, 0),
          expiresPrimaryText: "in 10d",
          expiresSecondaryText: "Apr 03, 2026",
        },
      ],
      sortDir: "asc",
      sortKey: "client",
    })
  );

  assert.match(markup, /Client/);
  assert.match(markup, /Status/);
  assert.match(markup, /Connections/);
  assert.match(markup, /Servers/);
  assert.match(markup, /Traffic/);
  assert.match(markup, /Quota/);
  assert.match(markup, /Expires/);
  assert.match(markup, /Pending Update/);
  assert.match(markup, /1\.5 GB \/ 5\.0 GB/);
  assert.match(markup, /in 10d/);
  assert.match(markup, /Apr 03, 2026/);
  assert.match(markup, /data-slot="client-footer"/);
});
