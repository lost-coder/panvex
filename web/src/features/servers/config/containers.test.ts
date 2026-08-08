import { describe, expect, it } from "vitest";
import { readUpstreams, writeUpstreams, readMap, writeMap } from "./containers";

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
      "hv24s.metrion.icu": ["127.0.0.1:8085"],
    });
  });

  it("пишет таблицу, сохраняя точки в ключе целиком", () => {
    const out: Record<string, unknown> = {};
    writeMap(out, "censorship.exclusive_mask", { "hv24s.metrion.icu": ["127.0.0.1:8085"] });
    expect(out).toEqual({
      censorship: { exclusive_mask: { "hv24s.metrion.icu": "127.0.0.1:8085" } },
    });
  });

  it("dc_overrides сохраняет форму массива адресов", () => {
    const sections = { dc_overrides: { "203": ["91.105.192.100:443"] } };
    expect(readMap(sections, "dc_overrides")).toEqual({ "203": ["91.105.192.100:443"] });

    const out: Record<string, unknown> = {};
    writeMap(out, "dc_overrides", { "203": ["91.105.192.100:443", "1.2.3.4:443"] });
    expect(out).toEqual({ dc_overrides: { "203": ["91.105.192.100:443", "1.2.3.4:443"] } });
  });
});
