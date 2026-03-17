import test from "node:test";
import assert from "node:assert/strict";

import {
  emptyClientForm,
  filterAssignmentOptions,
  formToClientInput,
  summarizeAssignmentSelection,
  summarizeAssignments,
  validateClientForm
} from "../src/clients-form-state.ts";

test("emptyClientForm defaults to automatic ad tag generation", () => {
  const form = emptyClientForm();

  assert.equal(form.adTagMode, "auto");
  assert.equal(form.enabled, true);
});

test("validateClientForm requires a client name and at least one assignment target", () => {
  const errors = validateClientForm({
    ...emptyClientForm(),
    name: "   "
  });

  assert.equal(errors.name, "Client name is required.");
  assert.equal(errors.assignments, "Select at least one fleet group or node.");
});

test("validateClientForm requires a 32-character hex ad tag in manual mode", () => {
  const errors = validateClientForm({
    ...emptyClientForm(),
    name: "alice",
    adTagMode: "manual",
    userADTag: "not-hex",
    agentIDs: ["agent-1"]
  });

  assert.equal(errors.userADTag, "Ad tag must contain exactly 32 hexadecimal characters.");
});

test("formToClientInput omits the ad tag when automatic generation is selected", () => {
  const payload = formToClientInput({
    ...emptyClientForm(),
    name: "alice",
    agentIDs: ["agent-1"],
    userADTag: "0123456789abcdef0123456789abcdef",
    adTagMode: "auto"
  });

  assert.equal(payload.user_ad_tag, "");
});

test("formToClientInput keeps the ad tag in manual mode", () => {
  const payload = formToClientInput({
    ...emptyClientForm(),
    name: "alice",
    agentIDs: ["agent-1"],
    adTagMode: "manual",
    userADTag: "0123456789abcdef0123456789abcdef"
  });

  assert.equal(payload.user_ad_tag, "0123456789abcdef0123456789abcdef");
});

test("filterAssignmentOptions matches case-insensitively and hides selected items", () => {
  const options = [
    { id: "agent-eu-01", label: "fra-gw-01 (eu-core)" },
    { id: "agent-eu-02", label: "ams-gw-01 (eu-core)" },
    { id: "agent-us-01", label: "iad-gw-01 (us-east)" }
  ];

  const result = filterAssignmentOptions(options, "GW-01", ["agent-eu-01"]);

  assert.deepEqual(result, [
    { id: "agent-eu-02", label: "ams-gw-01 (eu-core)" },
    { id: "agent-us-01", label: "iad-gw-01 (us-east)" }
  ]);
});

test("summarizeAssignments reports union coverage across groups and explicit nodes", () => {
  const summary = summarizeAssignments(
    {
      ...emptyClientForm(),
      fleetGroupIDs: ["qa"],
      agentIDs: ["agent-edge-01"]
    },
    [
      { id: "agent-eu-01", fleet_group_id: "eu-core" },
      { id: "agent-us-01", fleet_group_id: "us-east" },
      { id: "agent-qa-01", fleet_group_id: "qa" },
      { id: "agent-edge-01", fleet_group_id: "edge" }
    ]
  );

  assert.deepEqual(summary, {
    fleetGroupCount: 1,
    explicitNodeCount: 1,
    coveredNodeCount: 2
  });
});

test("summarizeAssignmentSelection keeps the row compact with overflow text", () => {
  const summary = summarizeAssignmentSelection(
    [
      { id: "eu-core", label: "eu-core" },
      { id: "us-east", label: "us-east" },
      { id: "edge", label: "edge" }
    ],
    ["eu-core", "us-east", "edge"]
  );

  assert.equal(summary, "eu-core, us-east +1 more");
});
