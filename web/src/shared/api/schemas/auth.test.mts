// @ts-nocheck
import assert from "node:assert/strict";
import test from "node:test";

import { loginResponseSchema, meResponseSchema } from "./auth.ts";

test("meResponseSchema accepts a well-formed viewer payload", () => {
  const parsed = meResponseSchema.parse({
    id: "u-1",
    username: "alice",
    role: "viewer",
    totp_enabled: false,
  });
  assert.equal(parsed.role, "viewer");
});

test("meResponseSchema rejects an unknown role — prevents silent auth bypass", () => {
  const result = meResponseSchema.safeParse({
    id: "u-1",
    username: "alice",
    role: "superuser",
    totp_enabled: false,
  });
  assert.equal(result.success, false);
});

test("meResponseSchema rejects missing totp_enabled — backend drift guard", () => {
  const result = meResponseSchema.safeParse({
    id: "u-1",
    username: "alice",
    role: "admin",
    // totp_enabled omitted
  });
  assert.equal(result.success, false);
});

test("loginResponseSchema accepts { status }", () => {
  const parsed = loginResponseSchema.parse({ status: "ok" });
  assert.equal(parsed.status, "ok");
});

test("loginResponseSchema rejects empty object", () => {
  const result = loginResponseSchema.safeParse({});
  assert.equal(result.success, false);
});
