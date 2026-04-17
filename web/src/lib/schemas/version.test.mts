// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { isOperatorVersion, versionSchema } from "./version.ts";

test("versionSchema accepts viewer payload (no build fingerprint)", () => {
  const parsed = versionSchema.parse({ version: "1.2.3" });
  assert.equal(parsed.version, "1.2.3");
  assert.equal(isOperatorVersion(parsed), false);
});

test("versionSchema accepts operator payload and narrows", () => {
  const parsed = versionSchema.parse({
    version: "1.2.3",
    commit_sha: "deadbeef",
    build_time: "2024-01-01T00:00:00Z",
  });
  assert.equal(isOperatorVersion(parsed), true);
  if (isOperatorVersion(parsed)) {
    assert.equal(parsed.commit_sha, "deadbeef");
  }
});

test("versionSchema rejects missing version field", () => {
  const result = versionSchema.safeParse({ commit_sha: "abc" });
  assert.equal(result.success, false);
});

test("versionSchema rejects non-string version (e.g. backend regression to number)", () => {
  const result = versionSchema.safeParse({ version: 123 });
  assert.equal(result.success, false);
});
