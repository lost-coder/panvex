import assert from "node:assert/strict";
import test from "node:test";

import { getSettingsTabs, getSidebarNavigation, getUserMenuItems } from "../src/profile-and-settings-state.ts";

test("admin sidebar keeps settings while viewer sidebar stays operational only", () => {
  const adminNavigation = getSidebarNavigation("admin");
  const viewerNavigation = getSidebarNavigation("viewer");

  assert.equal(adminNavigation.some((item) => item.to === "/settings"), true);
  assert.equal(viewerNavigation.some((item) => item.to === "/settings"), false);
  assert.equal(viewerNavigation.some((item) => item.to === "/profile"), false);
});

test("user menu exposes profile and logout actions", () => {
  assert.deepEqual(getUserMenuItems(), [
    { kind: "link", label: "Profile", to: "/profile" },
    { kind: "action", label: "Log out", action: "logout" }
  ]);
});

test("settings tabs stay admin-only shared sections", () => {
  assert.deepEqual(
    getSettingsTabs("admin").map((tab) => tab.id),
    ["panel", "users"]
  );
  assert.deepEqual(getSettingsTabs("viewer"), []);
});
