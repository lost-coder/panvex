// Чтение/запись контейнеров конфига Telemt.
//
// Контейнер — узел, чьи дочерние ключи задаёт оператор, а не схема:
// upstreams (массив таблиц), dc_overrides ("203" -> ip:port) и
// censorship.exclusive_mask ("hv24s.metrion.icu" -> ip:port). Ключи двух
// последних сами содержат точки, поэтому адресовать их dotted-путём нельзя:
// "censorship.exclusive_mask.hv24s.metrion.icu" неразличимо разбирается на
// сегменты. Отсюда отдельный модуль вместо sections.ts.
//
// readMap/writeMap переносят форму значения БЕЗ преобразования — они просто
// сериализуют то, что им дали. Какую форму дать решает MapEditor, и решает
// её по ТИПУ контейнера в Telemt, а не по тому, что было раньше:
// dc_overrides — HashMap<String, Vec<String>>, censorship.exclusive_mask —
// HashMap<String, String> (см. MapEditor.tsx). На живой ноде поэтому
// встречаются обе формы одновременно — dc_overrides.203 массивом из одного
// элемента, exclusive_mask скалярами, — и readMap читает их как есть.

import { getPath, setPath } from "./sections";

export type UpstreamEntry = Record<string, unknown>;

/** Значение map-контейнера ровно в той форме, в какой оно лежит в конфиге. */
export type MapValue = string | string[];

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

/**
 * Есть ли в map-контейнере хотя бы одна запись, которая переживёт writeMap
 * (непустой ключ и непустое после trim значение). Используется вместо
 * сырого `Object.keys(map).length > 0` там, где нужно решить, писать ли
 * контейнер вовсе: пустая строка, которую заводит кнопка «Добавить»
 * (`{"": ""}`), инфлирует счёт ключей до 1, хотя writeMap эту запись сама
 * отбросит — сырой счёт увидел бы контейнер «непустым» и создал бы
 * пустой, но присутствующий раздел там, где оператор ничего не ввёл.
 */
export function mapHasContent(map: Record<string, MapValue>): boolean {
  return Object.entries(map).some(([key, value]) => {
    if (key.trim() === "") return false;
    return Array.isArray(value) ? value.some((s) => s.trim() !== "") : value.trim() !== "";
  });
}

/** Читает map-контейнер, сохраняя форму каждого значения (скаляр или массив). */
export function readMap(
  sections: Record<string, unknown>,
  path: string,
): Record<string, MapValue> {
  const raw = getPath(sections, path);
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) return {};
  const out: Record<string, MapValue> = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    out[key] = Array.isArray(value) ? value.map(String) : String(value);
  }
  return out;
}

/** Пишет map-контейнер, сохраняя форму значений; пустые записи опускает. */
export function writeMap(
  sections: Record<string, unknown>,
  path: string,
  map: Record<string, MapValue>,
): void {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(map)) {
    if (key.trim() === "") continue;
    if (Array.isArray(value)) {
      const clean = value.map((s) => s.trim()).filter((s) => s.length > 0);
      if (clean.length === 0) continue;
      out[key] = clean;
    } else {
      const clean = value.trim();
      if (clean === "") continue;
      out[key] = clean;
    }
  }
  setPath(sections, path, out);
}
