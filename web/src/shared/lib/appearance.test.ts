import assert from "node:assert/strict";
import test from "node:test";

import {
  buildAppearanceDraft,
  defaultAppearanceSettings,
  normalizeAppearanceSettings,
} from "./appearance.ts";

void test("appearance helpers default and normalize help_mode", () => {
  assert.equal(defaultAppearanceSettings.help_mode, "basic");

  const normalized = normalizeAppearanceSettings({
    theme: "dark",
    density: "compact",
    help_mode: "full",
    updated_at_unix: 10,
  });

  assert.equal(normalized.help_mode, "full");

  // Q5.U-Q-24: feed an out-of-range help_mode to verify the fallback
  // path. The cast is the test's purpose — it would otherwise refuse to
  // compile under strict TS, which is the whole point: real callers
  // can't pass "verbose"; we still want defensive normalisation in
  // buildAppearanceDraft for malformed payloads on the wire.
  const fallback = buildAppearanceDraft({
    theme: "light",
    density: "comfortable",
    help_mode: "verbose" as "basic",
  });

  assert.equal(fallback.helpMode, "basic");
});
