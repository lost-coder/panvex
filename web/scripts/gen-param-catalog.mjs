import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";

export function parseTables(markdown) {
  const rows = new Map();
  let section = null;
  for (const line of markdown.split("\n")) {
    const sec = line.match(/^# \[+([a-z_][\w.]*)\]+\s*$/);
    if (sec) { section = sec[1]; continue; }
    if (/^# Top-level keys\s*$/.test(line)) { section = ""; continue; }
    if (/^# /.test(line)) { section = null; continue; }
    const row = line.match(/^\|\s*\[`([^`]+)`\]\([^)]*\)\s*\|\s*(.+?)\s*\|\s*(.*?)\s*\|\s*`?([✔✘])`?\s*\|/);
    if (row && section !== null) {
      const [, key, type, def, hot] = row;
      const path = section ? `${section}.${key}` : key;
      rows.set(path, { type: type.trim(), default: def.replace(/`/g, "").trim(), hot: hot === "✔" });
    }
  }
  return rows;
}

export function parseEnumOptions(typeCell) {
  const opts = [...typeCell.matchAll(/`"([^"]+)"`/g)].map((m) => m[1]);
  return opts.length >= 2 ? opts : null;
}

export function parseDescriptions(markdown) {
  const desc = new Map();
  let section = null, cur = null;
  for (const line of markdown.split("\n")) {
    const sec = line.match(/^# \[+([a-z_][\w.]*)\]+\s*$/);
    if (sec) { section = sec[1]; cur = null; continue; }
    // Bracketless dotted sub-heading (e.g. malformed "# censorship.tls_fetch" in
    // the RU doc, where EN correctly has "# [censorship.tls_fetch]"): targets the
    // description of that exact field without touching the current section, so
    // sibling "## key" headings right after it still resolve relative to it.
    const secBare = line.match(/^# ([a-z_][\w.]*)\s*$/);
    if (secBare && section !== null) { cur = secBare[1]; continue; }
    if (/^# /.test(line)) {
      section = /^# (Top-level keys|Ключи верхнего уровня)\s*$/.test(line) ? "" : null;
      cur = null;
      continue;
    }
    // h2 headings sometimes carry a disambiguating parenthetical, e.g.
    // "## ipv4 (upstreams)" — capture just the key.
    const h2 = line.match(/^## ([a-z_][\w.]*)(?:\s+\([^)]*\))?\s*$/);
    if (h2 && section !== null) { cur = section ? `${section}.${h2[1]}` : h2[1]; continue; }
    const m = line.match(/^\s*-\s*\*\*(?:Description|Описание)\*\*:\s*(.+)$/);
    if (m && cur) desc.set(cur, m[1].trim());
  }
  return desc;
}

const EDITABLE = ["general", "timeouts", "censorship", "upstreams", "dc_overrides"];
const NUMERIC = /^(u8|u16|u32|u64|usize|i8|i16|i32|i64|f32|f64)$/;
// Matches the doc's array rendering exactly: a bare element type name
// (String, IpNetwork, IpAddr, Table, ...) followed by a trailing `[]`, and
// NOTHING else — anchored full-match so union cells like `"*"` or `String[]`
// (show_link / general.links.show) are deliberately excluded: those are not
// plain arrays, they're a literal-or-array union, and must keep falling
// through to "string" like before.
const ARRAY = /^[A-Za-z][A-Za-z0-9]*\[\]$/;

function fieldType(typeCell, options) {
  // Checked before the enum/select branch: a Type cell that renders as an
  // array of quoted-enum values would still need to come out as "string[]"
  // here, not get misread as a scalar "select" by parseEnumOptions.
  const bare = typeCell.replace(/`/g, "").trim();
  if (ARRAY.test(bare)) return "string[]";
  if (options) return "select";
  if (bare === "bool") return "boolean";
  if (NUMERIC.test(bare)) return "number";
  return "string";
}

export function buildCatalog(enMd, ruMd, tag) {
  const tables = parseTables(enMd);
  const dEn = parseDescriptions(enMd);
  const dRu = parseDescriptions(ruMd);
  const fields = [];
  for (const [path, row] of tables) {
    const section = path.split(".")[0];
    if (!EDITABLE.includes(section)) continue;
    const options = parseEnumOptions(row.type);
    const entry = {
      path, section,
      type: fieldType(row.type, options),
      applyMode: row.hot ? "hot" : "reload",
      en: dEn.get(path) ?? "", ru: dRu.get(path) ?? "",
    };
    if (options) entry.options = options;
    let def = row.default === "—" ? "" : row.default;
    if (def.length >= 2 && def.startsWith('"') && def.endsWith('"')) def = def.slice(1, -1);
    if (def) entry.default = def;
    fields.push(entry);
  }
  fields.sort((a, b) =>
    EDITABLE.indexOf(a.section) - EDITABLE.indexOf(b.section) || a.path.localeCompare(b.path));
  return { version: tag, fields };
}

export async function fetchAndVerify(source, fetchImpl = fetch) {
  const out = {};
  for (const [lang, f] of Object.entries(source.files)) {
    const url = `https://raw.githubusercontent.com/${source.repo}/${source.tag}/${f.path}`;
    const res = await fetchImpl(url);
    if (!res.ok) throw new Error(`download failed: ${url}`);
    const body = await res.text();
    const got = createHash("sha256").update(body).digest("hex");
    if (got !== f.sha256) throw new Error(`sha256 mismatch: ${f.path} (got ${got})`);
    out[lang] = body;
  }
  return out;
}

async function main() {
  const src = JSON.parse(readFileSync(new URL("./telemt-doc-sources.json", import.meta.url), "utf8"));
  const { en, ru } = await fetchAndVerify(src);
  const catalog = buildCatalog(en, ru, src.tag);
  const dest = new URL("../src/features/servers/config/paramCatalog.gen.json", import.meta.url);
  writeFileSync(dest, JSON.stringify(catalog, null, 2) + "\n");
  console.log(`param catalog: ${catalog.fields.length} fields @ ${catalog.version}`);
}

if (import.meta.url === `file://${process.argv[1]}`) main().catch((e) => { console.error(e); process.exit(1); });
