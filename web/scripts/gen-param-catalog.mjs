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
    if (/^# /.test(line)) { section = /^# Top-level keys/.test(line) ? "" : null; cur = null; continue; }
    const h2 = line.match(/^## ([a-z_][\w.]*)\s*$/);
    if (h2 && section !== null) { cur = section ? `${section}.${h2[1]}` : h2[1]; continue; }
    const m = line.match(/^\s*-\s*\*\*(?:Description|Описание)\*\*:\s*(.+)$/);
    if (m && cur) desc.set(cur, m[1].trim());
  }
  return desc;
}
