// Чтение/запись контейнеров конфига Telemt.
//
// Контейнер — узел, чьи дочерние ключи задаёт оператор, а не схема:
// upstreams (массив таблиц), dc_overrides ("203" -> ip:port) и
// censorship.exclusive_mask ("hv24s.metrion.icu" -> ip:port). Ключи двух
// последних сами содержат точки, поэтому адресовать их dotted-путём нельзя:
// "censorship.exclusive_mask.hv24s.metrion.icu" неразличимо разбирается на
// сегменты. Отсюда отдельный модуль вместо sections.ts.
//
// Значения map-контейнеров нормализуются к string[]: Telemt принимает и
// скаляр, и массив ip:port (dc_overrides документирован как "one or more"),
// а редактору удобнее одна форма. На запись список из одного элемента
// сворачивается обратно в скаляр — чтобы Save не порождал ложный дрейф
// против конфига, где записан скаляр.

import { getPath, setPath } from "./sections";

export type UpstreamEntry = Record<string, unknown>;

/** Читает массив upstreams. Отсутствие или чужая форма -> пустой список. */
export function readUpstreams(sections: Record<string, unknown>): UpstreamEntry[] {
  const raw = sections["upstreams"];
  if (!Array.isArray(raw)) return [];
  return raw.filter((e): e is UpstreamEntry => e !== null && typeof e === "object" && !Array.isArray(e));
}

/** Пишет массив upstreams как есть. */
export function writeUpstreams(sections: Record<string, unknown>, list: UpstreamEntry[]): void {
  sections["upstreams"] = list;
}

/** Читает map-контейнер, нормализуя значения к string[]. */
export function readMap(
  sections: Record<string, unknown>,
  path: string,
): Record<string, string[]> {
  const raw = getPath(sections, path);
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return {};
  const out: Record<string, string[]> = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    out[key] = Array.isArray(value) ? value.map(String) : [String(value)];
  }
  return out;
}

/** Пишет map-контейнер; список из одного элемента сворачивается в скаляр. */
export function writeMap(
  sections: Record<string, unknown>,
  path: string,
  map: Record<string, string[]>,
): void {
  const out: Record<string, unknown> = {};
  for (const [key, list] of Object.entries(map)) {
    const clean = list.map((s) => s.trim()).filter((s) => s.length > 0);
    if (clean.length === 0) continue;
    out[key] = clean.length === 1 ? clean[0] : clean;
  }
  setPath(sections, path, out);
}
