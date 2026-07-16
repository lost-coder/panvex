import { describe, expect, it } from "vitest";

import { API_ERROR_CODES } from "./error-codes";

describe("API_ERROR_CODES", () => {
  it("contains the known backend codes", () => {
    expect(API_ERROR_CODES).toContain("invalid_credentials");
    expect(API_ERROR_CODES).toContain("totp_required");
    expect(API_ERROR_CODES).toContain("session_store_unavailable");
    expect(API_ERROR_CODES).toContain("forbidden");
    expect(API_ERROR_CODES).toContain("rate_limited");
  });
});
