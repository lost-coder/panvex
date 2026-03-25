// @ts-nocheck
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import { clsx } from "clsx";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import * as ts from "typescript";
import { twMerge } from "tailwind-merge";

async function loadDataTable() {
  const componentPath = fileURLToPath(new URL("./data-table.tsx", import.meta.url));
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
    if (specifier === "@/lib/cn") {
      return {
        cn: (...inputs: unknown[]) => twMerge(clsx(inputs)),
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

  return module.exports.DataTable as (props: unknown) => React.ReactElement;
}

test("DataTable supports wrapper, column, row, mobile, and footer hooks", async () => {
  const DataTable = await loadDataTable();
  const markup = renderToStaticMarkup(
    React.createElement(DataTable, {
      columns: [
        {
          key: "server",
          label: "Server",
          headerClassName: "col-server-head",
          cellClassName: "col-server",
          render: (row: any) => row.server,
        },
        {
          key: "clients",
          label: "Clients",
          headerClassName: "col-clients-head",
          cellClassName: "col-clients",
          mobileLabel: "Clients",
          render: (row: any) => row.clients,
        },
      ],
      footer: React.createElement("div", { "data-slot": "table-footer" }, "footer"),
      headerRowClassName: "header-row",
      rowClassName: (row: any) => `row-${row.id}`,
      rows: [{ id: "alpha", server: "de-fra-01", clients: "342" }],
      tableClassName: "server-table-grid",
      wrapperClassName: "server-table-surface",
    })
  );

  assert.match(markup, /class="server-table-surface"/);
  assert.match(markup, /class="overflow-x-auto"/);
  assert.match(markup, /class="w-full text-sm server-table-grid"/);
  assert.match(markup, /class="border-b border-border header-row"/);
  assert.match(markup, /class="[^"]*col-server-head/);
  assert.match(markup, /class="[^"]*col-server/);
  assert.match(markup, /class="[^"]*row-alpha/);
  assert.match(markup, /data-mobile-label="Clients"/);
  assert.match(markup, /data-slot="table-footer"/);
});
