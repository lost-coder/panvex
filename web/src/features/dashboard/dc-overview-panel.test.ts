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

async function loadDcOverviewPanel() {
  const componentPath = fileURLToPath(new URL("./dc-overview-panel.tsx", import.meta.url));
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
    if (specifier === "@/components/section-panel") {
      return {
        SectionPanel: ({ title, children }) =>
          React.createElement("section", { "data-title": title }, children),
      };
    }

    if (specifier === "@/components/ui/dc-health-bar") {
      return {
        DcHealthBar: ({ segments }) =>
          React.createElement("div", { "data-slot": "health-bar" }, segments.join(",")),
      };
    }

    if (specifier === "./dashboard-view-model") {
      return {
        buildFleetDcCoverageSummary: () => ({
          totalDcCount: 2,
          okCount: 1,
          partialCount: 0,
          downCount: 1,
          rows: [
            {
              dc: 2,
              serverCount: 2,
              averageCoveragePct: 17,
              averageRttMs: 535,
              health: "down",
            },
            {
              dc: 4,
              serverCount: 1,
              averageCoveragePct: 100,
              averageRttMs: 18,
              health: "ok",
            },
          ],
        }),
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

  return module.exports.DcOverviewPanel as (props: unknown) => React.ReactElement;
}

test("DcOverviewPanel builds rows from runtime dcs and prioritizes unhealthy datacenters", async () => {
  const DcOverviewPanel = await loadDcOverviewPanel();
  const markup = renderToStaticMarkup(
    React.createElement(DcOverviewPanel, {
      agents: [
        {
          id: "server-a",
          runtime: {
            dcs: [
              {
                dc: 2,
                available_endpoints: 1,
                available_pct: 33,
                required_writers: 3,
                alive_writers: 1,
                coverage_pct: 33,
                rtt_ms: 420,
                load: 1,
              },
              {
                dc: 4,
                available_endpoints: 2,
                available_pct: 100,
                required_writers: 3,
                alive_writers: 3,
                coverage_pct: 100,
                rtt_ms: 18,
                load: 1,
              },
            ],
          },
        },
        {
          id: "server-b",
          runtime: {
            dcs: [
              {
                dc: 2,
                available_endpoints: 0,
                available_pct: 0,
                required_writers: 3,
                alive_writers: 0,
                coverage_pct: 0,
                rtt_ms: 650,
                load: 2,
              },
            ],
          },
        },
      ],
    })
  );

  assert.match(markup, /data-title="DC Degradation"/);
  assert.match(markup, />2</);
  assert.match(markup, /2 affected/);
  assert.match(markup, /535ms/);
  assert.match(markup, /down/i);
  assert.doesNotMatch(markup, />4</);
});
