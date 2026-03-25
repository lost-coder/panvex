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

function loadClientDetailPage(mocks) {
  const modulePath = fileURLToPath(new URL("./client-detail-page.tsx", import.meta.url));
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

  return module.exports.ClientDetailPage;
}

function createViewModel() {
  return {
    header: {
      nameText: "Acme Alpha",
      statusText: "Active",
      statusTone: "good",
      deploymentText: "1 failed, 1 pending",
      deploymentTone: "bad",
      metaItems: [
        { label: "Client ID", valueText: "client-1" },
        { label: "Targets", valueText: "3 rollout targets" },
        { label: "Expires", valueText: "Apr 03, 2026" },
      ],
    },
    overviewStats: [
      { label: "Traffic", valueText: "1.5 GB", secondaryText: "Current aggregated usage" },
      { label: "Active TCP", valueText: "12", secondaryText: "Reported active TCP sessions" },
      { label: "Assigned Nodes", valueText: "3", secondaryText: "Current rollout targets" },
      { label: "Quota", valueText: "1.5 GB / 5.0 GB", secondaryText: "Traffic used versus cap" },
      { label: "Expires", valueText: "in 9d", secondaryText: "Apr 03, 2026" },
      { label: "Deployment Health", valueText: "1 failed, 1 pending", secondaryText: "1 healthy rollout" },
    ],
    identityItems: [
      { label: "Name", valueText: "Acme Alpha" },
      { label: "Client ID", valueText: "client-1" },
      { label: "AD Tag", valueText: "ABCDEF0123456789ABCDEF0123456789" },
      { label: "State", valueText: "Enabled" },
    ],
    identitySecret: {
      maskedText: "telemt-se...0001",
      revealedText: "telemt-secret-0001",
    },
    usageItems: [
      { label: "Traffic used", valueText: "1.5 GB" },
      { label: "Unique IPs", valueText: "7" },
      { label: "Active TCP", valueText: "12" },
    ],
    limitItems: [
      { label: "Max TCP connections", valueText: "24" },
      { label: "Max unique IPs", valueText: "10" },
      { label: "Traffic quota", valueText: "5.0 GB" },
      { label: "Expiration", valueText: "Apr 03, 2026" },
    ],
    assignmentSummaryText: "2 fleet groups, 2 explicit nodes",
    assignmentGroups: ["eu-edge", "eu-backup"],
    assignmentAgents: ["agent-1", "agent-2"],
    deploymentRows: [
      {
        id: "dep-1",
        agentText: "agent-fra-01",
        statusText: "Failed",
        statusTone: "bad",
        desiredOperationText: "Update",
        lastAppliedText: "Not applied yet",
        linkText: "—",
        errorText: "telemt apply failed",
      },
    ],
  };
}

function createBaseMocks(overrides = {}) {
  return {
    "@tanstack/react-router": {
      useParams: () => ({ clientId: "client-1" }),
      useRouter: () => ({ history: { back: () => undefined } }),
    },
    "./client-detail.css": {},
    "./client-detail-hero": {
      ClientDetailHero: ({ header, onBack }) =>
        React.createElement(
          "section",
          { className: "client-detail-hero", "data-back-handler": typeof onBack === "function" },
          [
            "Back to Clients",
            header.nameText,
            header.statusText,
            header.deploymentText,
            "Client ID",
            "Targets",
            "Expires",
          ].join("|")
        ),
    },
    "./client-detail-kpis": {
      ClientDetailKpis: ({ stats }) =>
        React.createElement(
          "section",
          { className: "client-detail-kpis" },
          stats.map((stat) => `${stat.label}|${stat.valueText}|${stat.secondaryText}`).join("||")
        ),
    },
    "./client-detail-identity-panel": {
      ClientDetailIdentityPanel: ({ items, secret, panelKey }) =>
        React.createElement(
          "section",
          { className: "client-detail-identity-panel" },
          [
            panelKey,
            ...items.map((item) => `${item.label}|${item.valueText}`),
            secret.maskedText,
          ].join("||")
        ),
    },
    "./client-detail-usage-panel": {
      ClientDetailUsagePanel: ({ items }) =>
        React.createElement(
          "section",
          { className: "client-detail-usage-panel" },
          items.map((item) => `${item.label}|${item.valueText}`).join("||")
        ),
    },
    "./client-detail-limits-panel": {
      ClientDetailLimitsPanel: ({ items }) =>
        React.createElement(
          "section",
          { className: "client-detail-limits-panel" },
          items.map((item) => `${item.label}|${item.valueText}`).join("||")
        ),
    },
    "./client-detail-assignments-panel": {
      ClientDetailAssignmentsPanel: ({ summaryText, groups, agents }) =>
        React.createElement(
          "section",
          { className: "client-detail-assignments-panel" },
          [summaryText, ...groups, ...agents].join("||")
        ),
    },
    "./client-detail-deployment-table": {
      ClientDetailDeploymentTable: ({ rows }) =>
        React.createElement(
          "section",
          { className: "client-detail-deployment-table" },
          rows.map((row) => `${row.agentText}|${row.statusText}|${row.errorText}`).join("||")
        ),
    },
    "./clients-state": {
      useClientDetail: () => ({
        data: { id: "client-1", name: "Acme Alpha" },
        isLoading: false,
        isError: false,
      }),
    },
    "./client-detail-view-model": {
      buildClientDetailViewModel: () => createViewModel(),
    },
    ...overrides,
  };
}

test("ClientDetailPage renders the approved read-first client detail layout", () => {
  const ClientDetailPage = loadClientDetailPage(createBaseMocks());

  const markup = renderToStaticMarkup(React.createElement(ClientDetailPage));

  assert.match(markup, /client-detail-page/);
  assert.match(markup, /client-detail-hero/);
  assert.match(markup, /client-detail-kpis/);
  assert.match(markup, /client-detail-page__secondary-grid/);
  assert.match(markup, /client-detail-page__tertiary-grid/);
  assert.match(markup, /client-detail-deployment-table/);
  assert.match(markup, /Back to Clients/);
  assert.match(markup, /Acme Alpha/);
  assert.match(markup, /Active/);
  assert.match(markup, /Traffic/);
  assert.match(markup, /Assigned Nodes/);
  assert.match(markup, /Identity &amp; Secret/);
  assert.match(markup, /client-detail-identity-panel">client-1\|\|Name\|Acme Alpha/);
  assert.match(markup, /Usage/);
  assert.match(markup, /Limits/);
  assert.match(markup, /Assignments/);
  assert.match(markup, /Deployment/);
  assert.match(markup, /telemt-se\.\.\.0001/);
  assert.match(markup, /2 fleet groups, 2 explicit nodes/);
  assert.match(markup, /agent-fra-01\|Failed\|telemt apply failed/);
  assert.match(
    markup,
    /client-detail-hero.*client-detail-kpis.*client-detail-page__secondary-grid.*Identity &amp; Secret.*Usage.*client-detail-page__tertiary-grid.*Limits.*Assignments.*Deployment/s
  );
});

test("ClientDetailPage renders a not-found state when the requested client is missing", () => {
  const ClientDetailPage = loadClientDetailPage(
    createBaseMocks({
      "./clients-state": {
        useClientDetail: () => ({
          data: undefined,
          isLoading: false,
          isError: false,
        }),
      },
    })
  );

  const markup = renderToStaticMarkup(React.createElement(ClientDetailPage));

  assert.match(markup, /Client not found/i);
});

test("ClientDetailPage renders an error state when the client query fails", () => {
  const ClientDetailPage = loadClientDetailPage(
    createBaseMocks({
      "./clients-state": {
        useClientDetail: () => ({
          data: undefined,
          isLoading: false,
          isError: true,
        }),
      },
    })
  );

  const markup = renderToStaticMarkup(React.createElement(ClientDetailPage));

  assert.match(markup, /Client data is unavailable/i);
});
