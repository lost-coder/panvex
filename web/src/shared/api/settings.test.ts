// 3.14: settingsApi.putSettingsValues previously built its request body via
// `JSON.stringify(updates)` directly, bypassing the encodeRequest(path,
// schema, payload) validation path every other mutation uses. This test
// pins two contracts:
//
//   1. A valid payload is still PUT to /api/settings/values as JSON.
//   2. An invalid payload (non-scalar value) throws ApiSchemaError instead
//      of silently reaching fetch() with an unvalidated body.
//
// Mirrors the fetch-mock + CSRF-seed harness in config.test.ts /
// clients.reset-quota.test.ts.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { __seedCSRFTokenForTesting, ApiSchemaError } from "./http";
import { settingsApi } from "./settings";

describe("settingsApi.putSettingsValues", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("PUTs to /api/settings/values with the encoded updates body", async () => {
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );

    await settingsApi.putSettingsValues({
      "auth.timeout": 120,
      "auth.mfa_required": true,
    });

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/settings/values");
    expect(call[1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(call[1].body as string)).toEqual({
      "auth.timeout": 120,
      "auth.mfa_required": true,
    });
  });

  it("rejects a non-scalar value via the request schema before hitting fetch", () => {
    // encodeRequest validates synchronously before the fetch call is ever
    // made, so the schema mismatch surfaces as a thrown error, not a
    // rejected promise.
    expect(() =>
      settingsApi.putSettingsValues({
        // @ts-expect-error -- exercising the runtime guard against a
        // caller that bypasses the TS type (e.g. from an `any`-typed form).
        "auth.timeout": { nested: true },
      }),
    ).toThrow(ApiSchemaError);

    expect(globalThis.fetch).not.toHaveBeenCalled();
  });
});

// Bug 2: putRetentionSettings/putGeoIPSettings previously built their
// request bodies via `JSON.stringify(settings)` directly, bypassing the
// encodeRequest(path, schema, payload) validation path every other
// mutation uses. These tests pin the same two contracts as
// putSettingsValues above.
describe("settingsApi.putRetentionSettings", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("PUTs to /api/settings/retention with the encoded settings body", async () => {
    const settings = {
      ts_raw_seconds: 86400,
      ts_hourly_seconds: 604800,
      ts_dc_seconds: 86400,
      ip_history_seconds: 2592000,
      event_history_seconds: 86400,
    };
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(JSON.stringify(settings), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );

    await settingsApi.putRetentionSettings(settings);

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/settings/retention");
    expect(call[1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(call[1].body as string)).toEqual(settings);
  });

  it("rejects a non-integer field via the request schema before hitting fetch", () => {
    expect(() =>
      settingsApi.putRetentionSettings({
        ts_raw_seconds: 86400,
        ts_hourly_seconds: 604800,
        ts_dc_seconds: 86400,
        ip_history_seconds: 2592000,
        // @ts-expect-error -- exercising the runtime guard against a
        // caller that bypasses the TS type.
        event_history_seconds: "not-a-number",
      }),
    ).toThrow(ApiSchemaError);

    expect(globalThis.fetch).not.toHaveBeenCalled();
  });
});

describe("settingsApi.putGeoIPSettings", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = vi.fn();
    __seedCSRFTokenForTesting("test-csrf-token");
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("PUTs to /api/settings/geoip with the encoded settings body", async () => {
    const settings = {
      mode: "auto" as const,
      city: { enabled: true, url: "", local_path: "" },
      asn: { enabled: false, url: "", local_path: "" },
    };
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          settings,
          state: {
            city: {},
            asn: {},
          },
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      ),
    );

    await settingsApi.putGeoIPSettings(settings);

    const call = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0]!;
    expect(call[0]).toBe("/api/settings/geoip");
    expect(call[1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(call[1].body as string)).toEqual(settings);
  });

  it("rejects an invalid mode via the request schema before hitting fetch", () => {
    expect(() =>
      settingsApi.putGeoIPSettings({
        // @ts-expect-error -- exercising the runtime guard against a
        // caller that bypasses the TS type.
        mode: "not-a-real-mode",
        city: { enabled: true, url: "", local_path: "" },
        asn: { enabled: false, url: "", local_path: "" },
      }),
    ).toThrow(ApiSchemaError);

    expect(globalThis.fetch).not.toHaveBeenCalled();
  });
});
