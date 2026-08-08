// Мост между вложенной формой config-секций (ConfigSections) и плоской
// { "dotted.path": value } картой, в которой работает редактор.
//
// ВАЖНО: адресация идёт по ПОЛНОМУ dotted-пути (entry.path), а не по паре
// section+key. Каталог кодирует вложенность прямо в path
// ("general.links.public_host"), и попытка разложить её на один уровень
// порождала плоский ключ "links.public_host" внутри секции general — Telemt
// такой ключ молча игнорирует (в его конфиг-структурах нет
// deny_unknown_fields), из-за чего Apply рапортовал успех, ничего не меняя.
//
// Оба направления ограничены путями каталога PARAM_CATALOG: flatten
// игнорирует неуправляемые ключи, unflatten пишет только курируемые пути.
// Контейнеры (upstreams / dc_overrides / censorship.exclusive_mask) сюда не
// попадают — их ключи задаёт оператор и могут содержать точки, см.
// containers.ts.

import { PARAM_CATALOG } from "./paramCatalog";

/** Читает лист по dotted-пути. undefined, если любого сегмента нет или путь упирается в скаляр. */
export function getPath(obj: Record<string, unknown>, path: string): unknown {
  let node: unknown = obj;
  for (const segment of path.split(".")) {
    if (node === null || typeof node !== "object" || Array.isArray(node)) return undefined;
    node = (node as Record<string, unknown>)[segment];
  }
  return node;
}

/** Есть ли лист по dotted-пути (в отличие от getPath отличает отсутствие от значения undefined). */
export function hasPath(obj: Record<string, unknown>, path: string): boolean {
  const segments = path.split(".");
  let node: unknown = obj;
  for (const segment of segments.slice(0, -1)) {
    if (node === null || typeof node !== "object" || Array.isArray(node)) return false;
    node = (node as Record<string, unknown>)[segment];
  }
  if (node === null || typeof node !== "object" || Array.isArray(node)) return false;
  return Object.prototype.hasOwnProperty.call(node, segments[segments.length - 1] as string);
}

/** Пишет лист по dotted-пути, создавая промежуточные таблицы. Мутирует obj. */
export function setPath(obj: Record<string, unknown>, path: string, value: unknown): void {
  const segments = path.split(".");
  let node = obj;
  for (const segment of segments.slice(0, -1)) {
    const next = node[segment];
    if (next === null || typeof next !== "object" || Array.isArray(next)) {
      node[segment] = {};
    }
    node = node[segment] as Record<string, unknown>;
  }
  node[segments[segments.length - 1] as string] = value;
}

/**
 * flattenSections разворачивает вложенный объект секций в
 * { "dotted.path": value } ТОЛЬКО для курируемых путей PARAM_CATALOG.
 */
export function flattenSections(sections: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of PARAM_CATALOG) {
    if (hasPath(sections, f.path)) out[f.path] = getPath(sections, f.path);
  }
  return out;
}

/**
 * unflattenPaths собирает обратно вложенный объект секций из
 * { "dotted.path": value }, только для курируемых путей. Пустые значения
 * (undefined / "") опускаются, чтобы не писать пустые override.
 */
export function unflattenPaths(values: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const known = new Set(PARAM_CATALOG.map((f) => f.path));
  for (const [path, value] of Object.entries(values)) {
    if (!known.has(path)) continue;
    if (value === undefined || value === "") continue;
    setPath(out, path, value);
  }
  return out;
}
