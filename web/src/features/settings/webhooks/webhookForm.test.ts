import { describe, expect, it } from "vitest";

import {
  createWebhookEndpointRequestSchema,
  updateWebhookEndpointRequestSchema,
} from "@/shared/api/schemas";

const baseValid = {
  name: "ops-slack",
  url: "https://hooks.example.com/T/A/B",
  event_filter: "audit.*,alert.fired",
  allow_private: false,
  enabled: true,
};

describe("createWebhookEndpointRequestSchema", () => {
  it("accepts a fully populated valid payload", () => {
    const r = createWebhookEndpointRequestSchema.safeParse({
      ...baseValid,
      secret: "super-secret",
    });
    expect(r.success).toBe(true);
  });

  it("rejects empty secret on create", () => {
    const r = createWebhookEndpointRequestSchema.safeParse({
      ...baseValid,
      secret: "",
    });
    expect(r.success).toBe(false);
  });

  it("rejects non-http(s) url scheme", () => {
    const r = createWebhookEndpointRequestSchema.safeParse({
      ...baseValid,
      secret: "k",
      url: "ftp://example.com",
    });
    expect(r.success).toBe(false);
  });

  it("rejects malformed event_filter entry", () => {
    const r = createWebhookEndpointRequestSchema.safeParse({
      ...baseValid,
      secret: "k",
      event_filter: "bad@@filter",
    });
    expect(r.success).toBe(false);
  });

  it("accepts an empty event_filter (matches all)", () => {
    const r = createWebhookEndpointRequestSchema.safeParse({
      ...baseValid,
      secret: "k",
      event_filter: "",
    });
    expect(r.success).toBe(true);
  });
});

describe("updateWebhookEndpointRequestSchema", () => {
  it("accepts empty secret (preserve existing)", () => {
    const r = updateWebhookEndpointRequestSchema.safeParse({
      ...baseValid,
      secret: "",
    });
    expect(r.success).toBe(true);
  });

  it("rejects oversized secret", () => {
    const r = updateWebhookEndpointRequestSchema.safeParse({
      ...baseValid,
      secret: "x".repeat(2000),
    });
    expect(r.success).toBe(false);
  });
});
