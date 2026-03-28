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

async function loadServerCardDetails() {
  const componentPath = fileURLToPath(
    new URL("./server-card-details.tsx", import.meta.url)
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

  const realRequire = createRequire(import.meta.url);
  const module = { exports: {} as Record<string, unknown> };
  const mockRequire = (specifier: string) => {
    if (specifier === "@tanstack/react-router") {
      return {
        Link: ({ to, params, className, children }) =>
          React.createElement(
            "a",
            {
              href: `${to.replace("$serverId", params.serverId)}`,
              className,
            },
            children
          ),
      };
    }

    if (specifier.includes("telemetry/help-metadata")) {
      return {
        getTelemetryFieldHelp: (label: string) => {
          if (label === "DC Health") {
            return "Aggregate Telegram data center coverage and health summary for the node.";
          }
          return undefined;
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
  const script = new vm.Script(compiled, { filename: componentPath });
  script.runInContext(context);

  return module.exports.ServerCardDetails as (props: unknown) => React.ReactElement;
}

test("ServerCardDetails renders the DC status table and detail link", async () => {
  const ServerCardDetails = await loadServerCardDetails();
  const markup = renderToStaticMarkup(
    React.createElement(ServerCardDetails, {
      details: {
        isOffline: false,
        rows: [
          {
            dc: 1,
            dcText: "1",
            rttText: "137ms",
            writersText: "4/3",
            coverageText: "67%",
            loadText: "1.5",
            health: "partial",
          },
        ],
      },
      serverId: "server-1",
      lastSeenText: "Last contact: 2 min ago",
    })
  );

  assert.match(markup, /DC Status/);
  assert.match(markup, /DC/);
  assert.match(markup, /RTT/);
  assert.match(markup, /Writers/);
  assert.match(markup, /Coverage/);
  assert.match(markup, /137ms/);
  assert.match(markup, /4\/3/);
  assert.match(markup, /67%/);
  assert.match(markup, /Open server page/);
  assert.match(markup, /href="\/servers\/server-1"/);
});

test("ServerCardDetails renders the offline empty state when dc data is unavailable", async () => {
  const ServerCardDetails = await loadServerCardDetails();
  const markup = renderToStaticMarkup(
    React.createElement(ServerCardDetails, {
      details: {
        isOffline: true,
        rows: [],
      },
      serverId: "server-2",
      lastSeenText: "Last contact: 15 min ago",
    })
  );

  assert.match(markup, /Offline/);
  assert.match(markup, /Server unavailable/);
  assert.match(markup, /DC data is unavailable/);
  assert.match(markup, /Last contact: 15 min ago/);
});
