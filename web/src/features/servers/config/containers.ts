// Чтение/запись контейнеров конфига Telemt.
//
// Контейнер — узел, чьи дочерние ключи задаёт оператор, а не схема:
// upstreams (массив таблиц), dc_overrides ("203" -> ip:port) и
// censorship.exclusive_mask ("hv24s.metrion.icu" -> ip:port). Ключи двух
// последних сами содержат точки, поэтому адресовать их dotted-путём нельзя:
// "censorship.exclusive_mask.hv24s.metrion.icu" неразличимо разбирается на
// сегменты. Отсюда отдельный модуль вместо sections.ts.
//
// Форма значений map-контейнеров переносится БЕЗ преобразования. Telemt
// принимает и скаляр, и массив ip:port (dc_overrides документирован как
// "one or more"), и на живой ноде встречаются обе формы одновременно:
// dc_overrides.203 записан массивом из одного элемента, exclusive_mask —
// скалярами. Нормализация к string[] с обратным сворачиванием одиночного
// списка в скаляр переписала бы такой массив при первом же Save, а
// configDrift сравнивает канонические байты — получился бы ложный дрейф без
// единой правки оператора. Разбор строки в список живёт в MapEditor, который
// сохраняет исходную форму поля.

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
