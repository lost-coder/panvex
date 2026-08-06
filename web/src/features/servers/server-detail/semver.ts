// Task 14: minimal semver comparison for the "Telemt X → Y" update badge.
// Telemt release tags are plain `major.minor.patch` (optionally `vX.Y.Z`),
// no pre-release/build metadata in practice — this intentionally does not
// implement the full semver spec (pre-release precedence, build metadata),
// just enough to order release tags. Missing/non-numeric segments compare
// as 0 so a malformed string never throws, it just sorts low.
function parseSemver(version: string): [number, number, number] {
  const bare = version.trim().replace(/^v/i, "");
  const [major = "0", minor = "0", patch = "0"] = bare.split(".");
  const toInt = (s: string) => {
    const n = Number.parseInt(s, 10);
    return Number.isFinite(n) ? n : 0;
  };
  return [toInt(major), toInt(minor), toInt(patch)];
}

/** -1 if a < b, 0 if equal, 1 if a > b (numeric major.minor.patch only). */
export function compareSemver(a: string, b: string): -1 | 0 | 1 {
  const [aMajor, aMinor, aPatch] = parseSemver(a);
  const [bMajor, bMinor, bPatch] = parseSemver(b);
  for (const [x, y] of [
    [aMajor, bMajor],
    [aMinor, bMinor],
    [aPatch, bPatch],
  ] as const) {
    if (x < y) return -1;
    if (x > y) return 1;
  }
  return 0;
}

/** True when `a` is strictly older than `b`. Blank inputs are never "less". */
export function semverLt(a: string, b: string): boolean {
  if (!a.trim() || !b.trim()) return false;
  return compareSemver(a, b) < 0;
}
