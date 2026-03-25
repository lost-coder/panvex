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

async function loadServerCardSummary() {
  const componentPath = fileURLToPath(
    new URL("./server-card-summary.tsx", import.meta.url)
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

  return module.exports.ServerCardSummary as (props: unknown) => React.ReactElement;
}

test("ServerCardSummary renders the template-style server overview", async () => {
  const ServerCardSummary = await loadServerCardSummary();
  const markup = renderToStaticMarkup(
    React.createElement(ServerCardSummary, {
      summary: {
        id: "server-1",
        nameText: "de-fra-01",
        locationText: "Frankfurt, DE",
        statusText: "Degraded",
        statusTone: "warn",
        metrics: [
          { label: "Clients", value: "342" },
          { label: "CPU", value: "—" },
          { label: "Traffic", value: "—" },
        ],
        dcCounts: {
          ok: 9,
          partial: 1,
          down: 2,
        },
        dcTags: ["ok", "ok", "partial", "down"],
      },
      expanded: false,
      hintText: "Press for DC details",
    })
  );

  assert.match(markup, /de-fra-01/);
  assert.match(markup, /Frankfurt, DE/);
  assert.match(markup, /Degraded/);
  assert.match(markup, /Clients/);
  assert.match(markup, />342</);
  assert.match(markup, /CPU/);
  assert.match(markup, /Traffic/);
  assert.match(markup, /OK/);
  assert.match(markup, /degraded/);
  assert.match(markup, /critical/);
  assert.match(markup, /Press for DC details/);
});

test("ServerCardSummary renders an offline summary hint without the expand CTA text", async () => {
  const ServerCardSummary = await loadServerCardSummary();
  const markup = renderToStaticMarkup(
    React.createElement(ServerCardSummary, {
      summary: {
        id: "server-2",
        nameText: "nl-ams-02",
        locationText: "Amsterdam, NL",
        statusText: "Offline",
        statusTone: "bad",
        metrics: [
          { label: "Clients", value: "—" },
          { label: "CPU", value: "—" },
          { label: "Traffic", value: "—" },
        ],
        dcCounts: {
          ok: 0,
          partial: 0,
          down: 12,
        },
        dcTags: ["down", "down", "down"],
      },
      expanded: false,
      hintText: "Server unavailable - last contact 15 min ago",
    })
  );

  assert.match(markup, /Server unavailable - last contact 15 min ago/);
  assert.doesNotMatch(markup, /Press for DC details/);
});
