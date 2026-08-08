import { describe, expect, it } from "vitest";
import { readUpstreams, writeUpstreams, readMap, writeMap, unmanagedMapEntries } from "./containers";

describe("контейнеры конфига", () => {
  it("читает и пишет массив upstreams", () => {
    const sections = { upstreams: [{ type: "direct", weight: 1, enabled: true }] };
    expect(readUpstreams(sections)).toEqual([{ type: "direct", weight: 1, enabled: true }]);

    const out: Record<string, unknown> = {};
    writeUpstreams(out, [{ type: "socks5", address: "1.2.3.4:1080" }]);
    expect(out).toEqual({ upstreams: [{ type: "socks5", address: "1.2.3.4:1080" }] });
  });

  it("отсутствующий upstreams читается как пустой список", () => {
    expect(readUpstreams({})).toEqual([]);
    expect(readUpstreams({ upstreams: "мусор" })).toEqual([]);
  });

  it("читает таблицу с точками в ключах, не разваливая ключ на сегменты", () => {
    const sections = {
      censorship: { exclusive_mask: { "hv24s.metrion.icu": "127.0.0.1:8085" } },
    };
    expect(readMap(sections, "censorship.exclusive_mask")).toEqual({
      "hv24s.metrion.icu": "127.0.0.1:8085",
    });
  });

  it("пишет таблицу, сохраняя точки в ключе целиком", () => {
    const out: Record<string, unknown> = {};
    writeMap(out, "censorship.exclusive_mask", { "hv24s.metrion.icu": "127.0.0.1:8085" });
    expect(out).toEqual({
      censorship: { exclusive_mask: { "hv24s.metrion.icu": "127.0.0.1:8085" } },
    });
  });

  // Форму значения НЕ трогаем: на живой ноде dc_overrides.203 записан массивом
  // из одного элемента, а exclusive_mask — скалярами. Свернуть одиночный
  // список в скаляр значило бы породить ложный дрейф при первом же Save.
  it("сохраняет массив из одного элемента массивом", () => {
    const sections = { dc_overrides: { "203": ["91.105.192.100:443"] } };
    expect(readMap(sections, "dc_overrides")).toEqual({ "203": ["91.105.192.100:443"] });

    const out: Record<string, unknown> = {};
    writeMap(out, "dc_overrides", { "203": ["91.105.192.100:443"] });
    expect(out).toEqual({ dc_overrides: { "203": ["91.105.192.100:443"] } });
  });

  it("round-trip не меняет форму ни скаляра, ни массива", () => {
    const sections = {
      dc_overrides: { "203": ["91.105.192.100:443"], "204": "1.2.3.4:443" },
    };
    const out: Record<string, unknown> = {};
    writeMap(out, "dc_overrides", readMap(sections, "dc_overrides"));
    expect(out).toEqual(sections);
  });

  it("пишет многоэлементный массив как массив", () => {
    const out: Record<string, unknown> = {};
    writeMap(out, "dc_overrides", { "203": ["91.105.192.100:443", "1.2.3.4:443"] });
    expect(out).toEqual({ dc_overrides: { "203": ["91.105.192.100:443", "1.2.3.4:443"] } });
  });

  it("отбрасывает пустые записи, но не трогает остальные", () => {
    const out: Record<string, unknown> = {};
    writeMap(out, "dc_overrides", { "203": "1.2.3.4:443", "": "", "204": [] });
    expect(out).toEqual({ dc_overrides: { "203": "1.2.3.4:443" } });
  });

  // D5: desired can carry the container with SOME keys while the node's
  // observed config has MORE — F7's whole-container fallback only fires
  // when desired lacks the container entirely, so a poorer-but-present
  // desired hid the node's other keys nowhere. unmanagedMapEntries surfaces
  // exactly those observed-only keys, for display — never mutating the
  // managed map itself.
  it("D5: unmanagedMapEntries возвращает ключи observed, которых нет в managed", () => {
    const managed = { "203": ["91.105.192.100:443"] };
    const observedSections = {
      dc_overrides: { "1": ["1.2.3.4:443"], "203": ["91.105.192.100:443"] },
    };
    expect(unmanagedMapEntries(managed, observedSections, "dc_overrides")).toEqual({
      "1": ["1.2.3.4:443"],
    });
  });

  it("D5: unmanagedMapEntries пуст, когда managed уже покрывает всё, что есть на ноде", () => {
    const managed = { "203": ["91.105.192.100:443"] };
    const observedSections = { dc_overrides: { "203": ["91.105.192.100:443"] } };
    expect(unmanagedMapEntries(managed, observedSections, "dc_overrides")).toEqual({});
  });

  it("нечужая форма контейнера не роняет чтение", () => {
    expect(readMap({ dc_overrides: "мусор" }, "dc_overrides")).toEqual({});
    expect(readMap({}, "dc_overrides")).toEqual({});
    expect(readUpstreams({ upstreams: ["мусор", { type: "direct" }] })).toEqual([
      { type: "direct" },
    ]);
  });
});
