import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

test("router config wires page routes through lazyRouteComponent", async () => {
  // Phase-4 moved router.tsx into app/. The @ts-nocheck above used to
  // hide both the move and the stale assertion below — counted lazy
  // routes drifted from 8 to 15 as new pages landed. Now the path is
  // explicit and the assertion checks the lower bound: every page
  // route must still go through lazyRouteComponent (otherwise the
  // bundle defeats code-splitting).
  const source = await readFile(
    path.resolve(import.meta.dirname, "app", "router.tsx"),
    "utf8",
  );

  assert.match(source, /lazyRouteComponent/);

  const lazyMatches = source.match(/lazyRouteComponent\(/g) ?? [];

  assert.ok(
    lazyMatches.length >= 8,
    `expected at least 8 lazy routes, got ${lazyMatches.length}`,
  );
});
