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
