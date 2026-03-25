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

async function loadActivityPanel() {
  const componentPath = fileURLToPath(new URL("./activity-panel.tsx", import.meta.url));
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

    if (specifier === "@/components/activity-feed") {
      return {
        ActivityFeed: ({ items, emptyMessage }) =>
          React.createElement(
            "div",
            { "data-slot": "activity-feed" },
            items.length > 0
              ? items.map((item) => `${item.text}|${item.time}|${item.status}`).join("||")
              : emptyMessage
          ),
      };
    }

    if (specifier === "./dashboard-view-model") {
      return {
        extractRecentRuntimeEvents: (agents) =>
          (agents[0]?.runtime?.recent_events ?? []).map((event) => ({
            id: `${agents[0].id}-${event.sequence}`,
            agentId: agents[0].id,
            agentName: agents[0].node_name,
            timestampUnix: event.timestamp_unix,
            eventType: event.event_type,
            context: event.context,
            summaryText: event.context,
            status: "good",
          })),
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

  return module.exports.ActivityPanel as (props: unknown) => React.ReactElement;
}

test("ActivityPanel uses runtime recent events and ignores stale top-level event fields", async () => {
  const ActivityPanel = await loadActivityPanel();
  const markup = renderToStaticMarkup(
    React.createElement(ActivityPanel, {
      agents: [
        {
          id: "server-a",
          node_name: "alpha",
          recent_events: [
            {
              sequence: 1,
              timestamp_unix: 5,
              event_type: "top_level_event",
              context: "stale top level",
            },
          ],
          runtime: {
            recent_events: [
              {
                sequence: 2,
                timestamp_unix: 20,
                event_type: "connect_success",
                context: "runtime event",
              },
            ],
          },
        },
      ],
    })
  );

  assert.match(markup, /data-title="Recent Activity"/);
  assert.match(markup, /alpha: runtime event/i);
  assert.doesNotMatch(markup, /stale top level/i);
});
