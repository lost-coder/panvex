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
