// @ts-nocheck

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

test("router config wires page routes through lazyRouteComponent", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "router.tsx"), "utf8");

  assert.match(source, /lazyRouteComponent/);

  const lazyMatches = source.match(/lazyRouteComponent\(/g) ?? [];

  assert.equal(lazyMatches.length, 8);
});
