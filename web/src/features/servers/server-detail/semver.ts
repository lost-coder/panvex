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

// Strict `major.minor.patch` (optionally `vX.Y.Z`) — NOT the same laxness
// parseSemver applies internally. semverLt uses this to reject anything
// that isn't a real release tag (a "dev" build, a bare git SHA, a
// pre-release suffix) rather than coercing the unparseable parts to 0:
// coercion would make "dev" or a short hex SHA compare as "less than"
// any real release (e.g. "dev" -> 0.0.0 < "3.4.25"), producing a false
// "update available" badge on dev builds.
const STRICT_SEMVER_PATTERN = /^\d+\.\d+\.\d+$/;

/**
 * Task 4 fix: exported so the version picker can detect an unparseable
 * `currentVersion` (blank, "dev", a git SHA) up front and switch to a
 * forced-install confirm instead of the normal/downgrade one — the agent's
 * downgrade gate is fail-closed and requires `allow_downgrade` for *any*
 * install when it can't parse its own reported version, not just an actual
 * downgrade.
 */
export function isStrictSemver(version: string): boolean {
  return STRICT_SEMVER_PATTERN.test(version.trim().replace(/^v/i, ""));
}

/**
 * True when `a` is strictly older than `b`. False whenever either side
 * isn't a well-formed `major.minor.patch` release tag — blank, "dev",
 * a git SHA, a pre-release suffix, etc. all compare as "not less than"
 * rather than being coerced into an ordering. This only gates the
 * update badge / update-available UI; it must never be used to decide
 * whether the update button itself is enabled (that's the strategy/probe
 * guard — the agent's own downgrade gate is authoritative there).
 */
export function semverLt(a: string, b: string): boolean {
  if (!isStrictSemver(a) || !isStrictSemver(b)) return false;
  return compareSemver(a, b) < 0;
}
