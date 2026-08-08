// Сверяет три среза конфига одной ноды: файл Telemt (через его же API),
// то, что панель отдаёт как observed, и то, что каталог параметров вообще
// способен показать. Автотесты этого не ловят — здесь нужен живой Telemt.
//
// Запуск:
//   node scripts/verify-config-fidelity.mjs <agentId> [panelUrl] [telemtUrl]
// Логин берётся из PANVEX_USER / PANVEX_PASS (по умолчанию admin/Qwerty123456).
import { readFileSync } from "node:fs";

const [, , agentId, panelUrl = "http://127.0.0.1:8080", telemtUrl = "http://127.0.0.1:9091"] =
  process.argv;
if (!agentId) {
  console.error("usage: node scripts/verify-config-fidelity.mjs <agentId> [panelUrl] [telemtUrl]");
  process.exit(2);
}

const catalog = JSON.parse(
  readFileSync(new URL("../src/features/servers/config/paramCatalog.gen.json", import.meta.url)),
);
const CONTAINERS = ["upstreams", "dc_overrides", "censorship.exclusive_mask"];
const isContainer = (p) => CONTAINERS.some((c) => p === c || p.startsWith(`${c}.`));

function flatten(obj, prefix = "") {
  const out = {};
  if (obj === null || typeof obj !== "object") return out;
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k;
    if (v !== null && typeof v === "object" && !Array.isArray(v)) Object.assign(out, flatten(v, path));
    else out[path] = v;
  }
  return out;
}

const telemt = await (await fetch(`${telemtUrl}/v1/config`)).json();

const login = await fetch(`${panelUrl}/api/auth/login`, {
  method: "POST",
  headers: { "Content-Type": "application/json", Origin: panelUrl },
  body: JSON.stringify({
    username: process.env.PANVEX_USER ?? "admin",
    password: process.env.PANVEX_PASS ?? "Qwerty123456",
  }),
});
if (!login.ok) throw new Error(`login failed: ${login.status}`);
const cookie = login.headers.getSetCookie().map((c) => c.split(";")[0]).join("; ");

const panel = await (await fetch(`${panelUrl}/api/agents/${agentId}/config`, {
  headers: { Cookie: cookie },
})).json();

const telemtFlat = flatten(telemt.data);
const observedFlat = flatten(panel.observed);
const catalogPaths = new Set(catalog.fields.map((f) => f.path));

const fail = [];
const onlyTelemt = Object.keys(telemtFlat).filter((p) => !(p in observedFlat));
const onlyPanel = Object.keys(observedFlat).filter((p) => !(p in telemtFlat));
const valueDiff = Object.keys(telemtFlat)
  .filter((p) => p in observedFlat)
  .filter((p) => JSON.stringify(telemtFlat[p]) !== JSON.stringify(observedFlat[p]));

if (onlyTelemt.length) fail.push(`панель не получила ${onlyTelemt.length} путей: ${onlyTelemt.slice(0, 5)}`);
if (onlyPanel.length) fail.push(`панель показывает лишние ${onlyPanel.length} путей: ${onlyPanel.slice(0, 5)}`);
if (valueDiff.length) fail.push(`значения расходятся в ${valueDiff.length} путях: ${valueDiff.slice(0, 5)}`);

// Каждый неконтейнерный путь ноды должен иметь запись каталога — иначе он
// отрисуется как "unknown parameter" и станет нередактируемым.
const uncatalogued = Object.keys(observedFlat).filter((p) => !isContainer(p) && !catalogPaths.has(p));
if (uncatalogued.length) fail.push(`нет записи каталога для ${uncatalogued.length}: ${uncatalogued.slice(0, 10)}`);

// Каждая запись каталога должна быть адресуемой: её путь либо есть на ноде,
// либо отсутствует целиком (не задан) — но НЕ должен быть префиксом чужого
// пути, иначе это зонтичная запись поверх таблицы.
const umbrella = catalog.fields
  .map((f) => f.path)
  .filter((p) => Object.keys(observedFlat).some((o) => o.startsWith(`${p}.`)));
if (umbrella.length) fail.push(`зонтичные записи каталога поверх таблиц: ${umbrella}`);

console.log(`путей у Telemt: ${Object.keys(telemtFlat).length}`);
console.log(`путей у панели: ${Object.keys(observedFlat).length}`);
console.log(`записей каталога: ${catalog.fields.length}, схема upstream: ${catalog.upstreamFields.length}`);
console.log(
  `не задано на ноде (действует дефолт): ${
    catalog.fields.filter((f) => !(f.path in observedFlat)).length
  }`,
);

if (fail.length) {
  console.error("\nРАСХОЖДЕНИЯ:");
  for (const f of fail) console.error("  ✗ " + f);
  process.exit(1);
}
console.log("\n✓ панель отражает конфиг ноды один в один");
