import { describe, expect, it } from "vitest";

import { valuesEntrySchema, schemaEntrySchema } from "./settings";

describe("settings schema apply/override fields", () => {
  it("parses apply + overridden_by_env on a values entry", () => {
    const e = valuesEntrySchema.parse({
      value: ":8080",
      source: "env",
      locked: true,
      apply: "restart",
      overridden_by_env: true,
      env_var: "PANVEX_HTTP_ADDR",
    });
    expect(e.apply).toBe("restart");
    expect(e.overridden_by_env).toBe(true);
  });

  it("parses apply on a schema entry", () => {
    const s = schemaEntrySchema.parse({
      name: "http.listen_address",
      class: "operational",
      type: "hostport",
      apply: "restart",
      desc: "x",
    });
    expect(s.apply).toBe("restart");
  });
});
