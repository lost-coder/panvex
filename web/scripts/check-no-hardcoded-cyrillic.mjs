#!/usr/bin/env node
// Guard: no Cyrillic string literals in TS/TSX outside src/locales.
// All operator-facing copy must flow through i18next. Comments are
// stripped first (Russian design notes in comments are fine); tests and
// stories are skipped (they may assert localized output).
import { readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, relative, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SRC_DIR = resolve(__dirname, "..", "src");
const CYRILLIC = /[Ѐ-ӿ]/;
const SKIP_DIRS = new Set(["locales"]);

const isSkippedFile = (name) =>
  /\.(test|stories)\.(ts|tsx|mts)$/.test(name);

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      if (!SKIP_DIRS.has(entry)) yield* walk(full);
    } else if (/\.(ts|tsx|mts)$/.test(entry) && !isSkippedFile(entry)) {
      yield full;
    }
  }
}

// Strip /* … */ blocks and // line tails. The `[^:]` guard keeps URLs
// ("https://…") intact — their "//" is preceded by ":".
function stripComments(source) {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

let bad = 0;
for (const file of walk(SRC_DIR)) {
  const lines = stripComments(readFileSync(file, "utf8")).split("\n");
  lines.forEach((line, i) => {
    if (CYRILLIC.test(line)) {
      bad += 1;
      console.error(
        `src/${relative(SRC_DIR, file)}:${i + 1}: hardcoded Cyrillic: ${line.trim()}`,
      );
    }
  });
}

if (bad > 0) {
  console.error(
    `\ncyrillic guard FAILED: ${bad} line(s). Move the copy into src/locales/<lng>/*.json.`,
  );
  process.exit(1);
}
console.log("cyrillic guard OK — no hardcoded Cyrillic outside src/locales.");
