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

async function loadDashboardSummaryCard() {
  const componentPath = fileURLToPath(
    new URL("./dashboard-summary-card.tsx", import.meta.url)
  );
  const source = readFileSync(componentPath, "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
      jsx: ts.JsxEmit.React,
      esModuleInterop: true,
    },
    fileName: componentPath,
  }).outputText;
  const require = createRequire(import.meta.url);
  const module = { exports: {} as Record<string, unknown> };
  const context = vm.createContext({
    require,
    module,
    exports: module.exports,
    __filename: componentPath,
    __dirname: path.dirname(componentPath),
    process,
    console,
  });
  const script = new vm.Script(compiled, { filename: componentPath });
  script.runInContext(context);

  return module.exports.DashboardSummaryCard as (props: {
    label: string;
    value: string | number;
    secondaryText?: string;
    breakdownItems?: Array<{
      label: string;
      value: string | number;
      tone?: "neutral" | "good" | "warn" | "bad";
    }>;
  }) => React.ReactElement;
}

test("DashboardSummaryCard renders label and primary value", async () => {
  const DashboardSummaryCard = await loadDashboardSummaryCard();
  const markup = renderToStaticMarkup(
    React.createElement(DashboardSummaryCard, {
      label: "Servers",
      value: "12",
    })
  );

  assert.match(markup, /Servers/);
  assert.match(markup, />12</);
});

test("DashboardSummaryCard renders optional secondary text and breakdown items", async () => {
  const DashboardSummaryCard = await loadDashboardSummaryCard();
  const markup = renderToStaticMarkup(
    React.createElement(DashboardSummaryCard, {
      label: "Clients / Active Connections",
      value: "45",
      secondaryText: "19 active now",
      breakdownItems: [
        { label: "Online", value: "7" },
        { label: "Degraded", value: "3", tone: "warn" },
      ],
    })
  );

  assert.match(markup, /19 active now/);
  assert.match(markup, /Online/);
  assert.match(markup, /Online:\s*7/);
  assert.match(markup, /Degraded/);
  assert.match(markup, /Degraded:\s*3/);
});

test("DashboardSummaryCard omits optional sections when not provided", async () => {
  const DashboardSummaryCard = await loadDashboardSummaryCard();
  const markup = renderToStaticMarkup(
    React.createElement(DashboardSummaryCard, {
      label: "Traffic",
      value: "1.2 TB",
    })
  );

  assert.doesNotMatch(markup, /active now/i);
  assert.doesNotMatch(markup, /Online/);
  assert.doesNotMatch(markup, /Degraded/);
});
