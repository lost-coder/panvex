import assert from "node:assert/strict";
import test from "node:test";

import {
  loginResponseSchema,
  meResponseSchema,
  totpSetupResponseSchema,
  totpStatusResponseSchema,
} from "./auth.ts";

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

test("totpSetupResponseSchema accepts a well-formed setup payload", () => {
  const parsed = totpSetupResponseSchema.parse({
    secret: "JBSWY3DPEHPK3PXP",
    otpauth_url: "otpauth://totp/Panvex:alice?secret=JBSWY3DPEHPK3PXP&issuer=Panvex",
  });
  assert.equal(parsed.secret, "JBSWY3DPEHPK3PXP");
});

test("totpSetupResponseSchema rejects empty secret", () => {
  const result = totpSetupResponseSchema.safeParse({
    secret: "",
    otpauth_url: "otpauth://totp/Panvex:alice",
  });
  assert.equal(result.success, false);
});

test("totpStatusResponseSchema accepts both enabled states", () => {
  assert.equal(totpStatusResponseSchema.parse({ totp_enabled: true }).totp_enabled, true);
  assert.equal(totpStatusResponseSchema.parse({ totp_enabled: false }).totp_enabled, false);
});

test("totpStatusResponseSchema rejects non-boolean", () => {
  const result = totpStatusResponseSchema.safeParse({ totp_enabled: "yes" });
  assert.equal(result.success, false);
});
