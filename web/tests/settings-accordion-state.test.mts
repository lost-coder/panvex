import test from "node:test";
import assert from "node:assert/strict";

import { toggleAccordionSection } from "../src/components/settings-accordion-state.ts";
import { getDefaultSettingsTab } from "../src/settings-page-state.ts";

test("toggleAccordionSection opens a closed section", () => {
  assert.equal(toggleAccordionSection(null, "panel"), "panel");
});

test("toggleAccordionSection closes the same section on a second click", () => {
  assert.equal(toggleAccordionSection("panel", "panel"), null);
});

test("toggleAccordionSection switches to a different section", () => {
  assert.equal(toggleAccordionSection("panel", "users"), "users");
});

test("getDefaultSettingsTab opens Panel first for admins", () => {
  assert.equal(getDefaultSettingsTab("admin"), "panel");
});

test("getDefaultSettingsTab keeps Security first for non-admin users", () => {
  assert.equal(getDefaultSettingsTab("operator"), "security");
  assert.equal(getDefaultSettingsTab("viewer"), "security");
});
