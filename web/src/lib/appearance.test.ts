// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import {
  buildAppearanceDraft,
  defaultAppearanceSettings,
  normalizeAppearanceSettings,
} from "./appearance.ts";

test("appearance helpers default and normalize help_mode", () => {
  assert.equal(defaultAppearanceSettings.help_mode, "basic");

  const normalized = normalizeAppearanceSettings({
    theme: "dark",
    density: "compact",
    help_mode: "full",
    updated_at_unix: 10,
  });

  assert.equal(normalized.help_mode, "full");

  const fallback = buildAppearanceDraft({
    theme: "light",
    density: "comfortable",
    help_mode: "verbose",
  });

  assert.equal(fallback.helpMode, "basic");
});
