import assert from "node:assert/strict";
import test from "node:test";

import {
  applyAppearanceAttributes,
  buildAppearanceDraft,
  clearAppearanceAttributes,
  getAppearanceQueryKey,
  normalizeAppearanceSettings,
  resolveEffectiveAppearance,
  syncAppearanceDraft
} from "../src/lib/appearance.ts";

test("normalizeAppearanceSettings applies built-in defaults", () => {
  assert.deepEqual(normalizeAppearanceSettings(undefined), {
    theme: "system",
    density: "comfortable",
    updated_at_unix: 0
  });
});

test("normalizeAppearanceSettings cleans invalid persisted values", () => {
  assert.deepEqual(
    normalizeAppearanceSettings({
      theme: "sepia",
      density: "dense",
      updated_at_unix: 42
    }),
    {
      theme: "system",
      density: "comfortable",
      updated_at_unix: 42
    }
  );
});

test("resolveEffectiveAppearance respects system theme and compact density", () => {
  assert.deepEqual(
    resolveEffectiveAppearance(
      {
        theme: "system",
        density: "compact",
        updated_at_unix: 0
      },
      true
    ),
    {
      theme: "dark",
      density: "compact"
    }
  );

  assert.deepEqual(
    resolveEffectiveAppearance(
      {
        theme: "system",
        density: "comfortable",
        updated_at_unix: 0
      },
      false
    ),
    {
      theme: "light",
      density: "comfortable"
    }
  );
});

test("getAppearanceQueryKey scopes appearance cache by current user", () => {
  assert.deepEqual(getAppearanceQueryKey("user-1"), ["appearance-settings", "user-1"]);
  assert.deepEqual(getAppearanceQueryKey("user-2"), ["appearance-settings", "user-2"]);
  assert.notDeepEqual(getAppearanceQueryKey("user-1"), getAppearanceQueryKey("user-2"));
});

test("syncAppearanceDraft keeps unsaved local changes during background refresh", () => {
  assert.deepEqual(
    syncAppearanceDraft(
      {
        theme: "dark",
        density: "compact"
      },
      {
        theme: "system",
        density: "comfortable",
        updated_at_unix: 10
      },
      true
    ),
    {
      theme: "dark",
      density: "compact"
    }
  );

  assert.deepEqual(
    syncAppearanceDraft(
      buildAppearanceDraft(undefined),
      {
        theme: "light",
        density: "compact",
        updated_at_unix: 11
      },
      false
    ),
    {
      theme: "light",
      density: "compact"
    }
  );
});

test("appearance root attributes can be applied and cleaned up", () => {
  const target = {
    dataset: {} as Record<string, string | undefined>
  };

  applyAppearanceAttributes(target, {
    theme: "dark",
    density: "compact"
  });

  assert.deepEqual(target.dataset, {
    theme: "dark",
    density: "compact"
  });

  clearAppearanceAttributes(target);
  assert.deepEqual(target.dataset, {});
});
