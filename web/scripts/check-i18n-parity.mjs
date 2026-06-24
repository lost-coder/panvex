#!/usr/bin/env node
// i18n key-parity guard. Walks every namespace JSON under
// src/locales/<lng> and asserts that the set of (dot-flattened) keys is
// identical across all configured languages. Exits non-zero and prints
// the missing keys per namespace/language when they diverge, so a
// translator can't ship en without the matching ru (or vice versa).
//
// Pure Node, no deps — safe to run in CI before the heavier tsc/vite
// build. Plural-suffix keys (foo_one / foo_other / ru's _few/_many) are
// legitimately language-specific, so the base key is compared instead of
// each plural variant.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const LOCALES_DIR = resolve(__dirname, "..", "src", "locales");

// Languages live as subdirectories of locales/. Detect them so adding a
// third language needs no edit here.
const LANGUAGES = readdirSync(LOCALES_DIR).filter((entry) =>
  statSync(join(LOCALES_DIR, entry)).isDirectory(),
);

if (LANGUAGES.length < 2) {
  console.error(
    `i18n parity: need >= 2 languages under ${LOCALES_DIR}, found ${LANGUAGES.length}`,
  );
  process.exit(1);
}

// i18next CLDR plural suffixes. We normalise these away so a key that is
// pluralised in one language but not another (a valid CLDR difference)
// does not register as a parity violation.
const PLURAL_SUFFIXES = [
  "_zero",
  "_one",
  "_two",
  "_few",
  "_many",
  "_other",
];

function stripPluralSuffix(key) {
  for (const suffix of PLURAL_SUFFIXES) {
    if (key.endsWith(suffix)) return key.slice(0, -suffix.length);
  }
  return key;
}

// Flatten a nested translation object into a Set of dot-joined leaf keys,
// with plural suffixes normalised to their base key.
function flatten(obj, prefix = "", out = new Set()) {
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === "object" && !Array.isArray(v)) {
      flatten(v, path, out);
    } else {
      out.add(stripPluralSuffix(path));
    }
  }
  return out;
}

function namespacesFor(lng) {
  return readdirSync(join(LOCALES_DIR, lng))
    .filter((f) => f.endsWith(".json"))
    .map((f) => f.slice(0, -".json".length));
}

function loadKeys(lng, ns) {
  const file = join(LOCALES_DIR, lng, `${ns}.json`);
  const json = JSON.parse(readFileSync(file, "utf8"));
  return flatten(json);
}

// Union of every namespace seen in any language so a whole missing file
// is reported too.
const allNamespaces = new Set();
for (const lng of LANGUAGES) {
  for (const ns of namespacesFor(lng)) allNamespaces.add(ns);
}

let problems = 0;

for (const ns of [...allNamespaces].sort((a, b) => a.localeCompare(b))) {
  // Build per-language key sets (empty set if the file is absent).
  const keysByLng = {};
  for (const lng of LANGUAGES) {
    try {
      keysByLng[lng] = loadKeys(lng, ns);
    } catch {
      keysByLng[lng] = new Set();
      console.error(`[${ns}] missing namespace file for "${lng}"`);
      problems++;
    }
  }

  // Reference union of all keys across languages for this namespace.
  const union = new Set();
  for (const lng of LANGUAGES) for (const k of keysByLng[lng]) union.add(k);

  for (const lng of LANGUAGES) {
    const missing = [...union]
      .filter((k) => !keysByLng[lng].has(k))
      .sort((a, b) => a.localeCompare(b));
    if (missing.length > 0) {
      problems++;
      console.error(
        `[${ns}] "${lng}" is missing ${missing.length} key(s):`,
      );
      for (const k of missing) console.error(`    - ${k}`);
    }
  }
}

if (problems > 0) {
  console.error(`\ni18n parity check FAILED: ${problems} issue(s).`);
  process.exit(1);
}

console.log(
  `i18n parity OK — ${allNamespaces.size} namespace(s) consistent across [${LANGUAGES.join(", ")}].`,
);
