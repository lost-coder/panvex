#!/usr/bin/env node
// Событийный + reason-string parity guard (P3-3.3, аудит #22).
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "..", "..");
const read = (rel) => readFileSync(resolve(repoRoot, rel), "utf8");

function extractGoConsts(source, prefix) {
  const re = new RegExp(`\\b${prefix}\\w*\\s*=\\s*"((?:[^"\\\\]|\\\\.)*)"`, "g");
  const out = new Set();
  for (const match of source.matchAll(re)) out.add(JSON.parse(`"${match[1]}"`));
  return out;
}
function extractTsArray(source, arrayName) {
  const block = source.match(new RegExp(`${arrayName}\\s*=\\s*\\[([\\s\\S]*?)\\]`));
  if (!block) abort(`не нашёл массив ${arrayName}`);
  const out = new Set();
  for (const m of block[1].matchAll(/"((?:[^"\\]|\\.)*)"/g)) out.add(JSON.parse(`"${m[1]}"`));
  return out;
}
function extractTsObjectKeys(source, objectName) {
  const block = source.match(new RegExp(`${objectName}[^=]*=\\s*\\{([\\s\\S]*?)\\n\\};`));
  if (!block) abort(`не нашёл объект ${objectName}`);
  const out = new Set();
  for (const m of block[1].matchAll(/^\s*"((?:[^"\\]|\\.)*)"\s*:/gm)) out.add(JSON.parse(`"${m[1]}"`));
  return out;
}
function abort(msg) { console.error(`event-parity: ${msg}`); process.exit(1); }
let failed = false;
function fail(msg) { console.error(`event-parity: ${msg}`); failed = true; }

const goEvents = extractGoConsts(read("internal/controlplane/events/types.go"), "Type");
const tsEvents = extractTsArray(read("web/src/shared/events/event-types.ts"), "EVENT_TYPES");
for (const t of goEvents) if (!tsEvents.has(t)) fail(`тип ${t} есть в Go, отсутствует в event-types.ts`);
for (const t of tsEvents) if (!goEvents.has(t)) fail(`тип ${t} есть в event-types.ts, отсутствует в Go`);
if (goEvents.size === 0) fail("Go-список типов пуст — регэксп разошёлся с types.go");

const goReasons = extractGoConsts(read("internal/controlplane/telemetry/projections.go"), "Reason");
const reasonTextSource = read("web/src/ui/lib/reason-text.ts");
const tsReasonKeys = extractTsObjectKeys(reasonTextSource, "REASON_KEYS");
const tsPrefixMatch = reasonTextSource.match(/TELEMT_PREFIX\s*=\s*"((?:[^"\\]|\\.)*)"/);
if (!tsPrefixMatch) abort("не нашёл TELEMT_PREFIX в reason-text.ts");
tsReasonKeys.add(JSON.parse(`"${tsPrefixMatch[1]}"`));
for (const r of goReasons) {
  if (!tsReasonKeys.has(r)) fail(`reason-строка ${JSON.stringify(r)} есть в Go, нет ключа в reason-text.ts`);
}
if (goReasons.size === 0) fail("Go-список reason-строк пуст — регэксп разошёлся с projections.go");

if (failed) process.exit(1);
console.log(`event-parity: OK (${goEvents.size} event types, ${goReasons.size} reason strings)`);
