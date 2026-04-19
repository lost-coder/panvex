import { describe, expect, it } from "vitest";

import {
  agentBootstrapRequestSchema,
  agentCertificateRecoveryGrantRequestSchema,
  agentCertificateRecoveryRequestSchema,
  clientMutationRequestSchema,
  createEnrollmentTokenRequestSchema,
  createJobRequestSchema,
  createUserRequestSchema,
  loginRequestSchema,
  panelUpdateRequestSchema,
  renameAgentRequestSchema,
  updateAppearanceSettingsRequestSchema,
  updatePanelSettingsRequestSchema,
  updateSettingsRequestSchema,
  updateTotpRequestSchema,
  updateUserRequestSchema,
} from "./index";

describe("loginRequestSchema", () => {
  it("accepts username + password without totp", () => {
    expect(
      loginRequestSchema.parse({ username: "alice", password: "p" }),
    ).toEqual({ username: "alice", password: "p" });
  });

  it("accepts a 6-digit totp_code", () => {
    expect(
      loginRequestSchema.parse({
        username: "alice",
        password: "p",
        totp_code: "123456",
      }).totp_code,
    ).toBe("123456");
  });

  it("rejects empty username", () => {
    expect(() =>
      loginRequestSchema.parse({ username: "", password: "p" }),
    ).toThrow();
  });

  it("rejects non-digit totp_code", () => {
    expect(() =>
      loginRequestSchema.parse({
        username: "alice",
        password: "p",
        totp_code: "abcdef",
      }),
    ).toThrow();
  });

  it("rejects short totp_code", () => {
    expect(() =>
      loginRequestSchema.parse({
        username: "alice",
        password: "p",
        totp_code: "12345",
      }),
    ).toThrow();
  });
});

describe("updateTotpRequestSchema", () => {
  it("requires both password and 6-digit totp_code", () => {
    expect(
      updateTotpRequestSchema.parse({ password: "p", totp_code: "111222" }),
    ).toEqual({ password: "p", totp_code: "111222" });
  });

  it("rejects missing totp_code", () => {
    expect(() =>
      updateTotpRequestSchema.parse({ password: "p" }),
    ).toThrow();
  });
});

describe("createUserRequestSchema", () => {
  it.each(["viewer", "operator", "admin"] as const)("accepts role %s", (role) => {
    expect(
      createUserRequestSchema.parse({ username: "u", role, password: "p" }).role,
    ).toBe(role);
  });

  it("rejects unknown role", () => {
    expect(() =>
      createUserRequestSchema.parse({ username: "u", role: "root", password: "p" }),
    ).toThrow();
  });
});

describe("updateUserRequestSchema", () => {
  it("allows omitted new_password", () => {
    expect(
      updateUserRequestSchema.parse({ username: "u", role: "admin" }),
    ).toEqual({ username: "u", role: "admin" });
  });

  it("accepts new_password", () => {
    expect(
      updateUserRequestSchema.parse({
        username: "u",
        role: "operator",
        new_password: "pw",
      }).new_password,
    ).toBe("pw");
  });
});

describe("clientMutationRequestSchema", () => {
  it("fills defaults for optional fields", () => {
    const parsed = clientMutationRequestSchema.parse({ name: "c1" });
    expect(parsed.user_ad_tag).toBe("");
    expect(parsed.max_tcp_conns).toBe(0);
    expect(parsed.fleet_group_ids).toEqual([]);
    expect(parsed.agent_ids).toEqual([]);
  });

  it("accepts nullable enabled", () => {
    expect(
      clientMutationRequestSchema.parse({ name: "c1", enabled: null }).enabled,
    ).toBeNull();
    expect(
      clientMutationRequestSchema.parse({ name: "c1", enabled: true }).enabled,
    ).toBe(true);
  });

  it("rejects empty name", () => {
    expect(() => clientMutationRequestSchema.parse({ name: "" })).toThrow();
  });

  it("rejects negative quotas", () => {
    expect(() =>
      clientMutationRequestSchema.parse({ name: "c", max_tcp_conns: -1 }),
    ).toThrow();
  });
});

describe("renameAgentRequestSchema", () => {
  it("requires non-empty node_name", () => {
    expect(renameAgentRequestSchema.parse({ node_name: "node-1" }).node_name).toBe(
      "node-1",
    );
    expect(() => renameAgentRequestSchema.parse({ node_name: "" })).toThrow();
  });
});

describe("createEnrollmentTokenRequestSchema", () => {
  it("requires positive ttl_seconds", () => {
    expect(
      createEnrollmentTokenRequestSchema.parse({
        fleet_group_id: "g1",
        ttl_seconds: 60,
      }),
    ).toEqual({ fleet_group_id: "g1", ttl_seconds: 60 });
    expect(() =>
      createEnrollmentTokenRequestSchema.parse({
        fleet_group_id: "g1",
        ttl_seconds: 0,
      }),
    ).toThrow();
  });
});

describe("createJobRequestSchema", () => {
  const validActions = [
    "runtime.reload",
    "users.create",
    "client.create",
    "client.update",
    "client.delete",
    "client.rotate_secret",
    "telemetry.refresh_diagnostics",
    "agent.self-update",
  ] as const;

  it.each(validActions)("accepts action %s", (action) => {
    expect(
      createJobRequestSchema.parse({
        action,
        target_agent_ids: ["a1"],
        idempotency_key: "k1",
        ttl_seconds: 60,
      }).action,
    ).toBe(action);
  });

  it("rejects unknown action", () => {
    expect(() =>
      createJobRequestSchema.parse({
        action: "client.nuke",
        target_agent_ids: ["a1"],
        idempotency_key: "k",
        ttl_seconds: 60,
      }),
    ).toThrow();
  });

  it("requires at least one target agent", () => {
    expect(() =>
      createJobRequestSchema.parse({
        action: "runtime.reload",
        target_agent_ids: [],
        idempotency_key: "k",
        ttl_seconds: 60,
      }),
    ).toThrow();
  });
});

describe("updateAppearanceSettingsRequestSchema", () => {
  it("accepts all valid triples", () => {
    expect(
      updateAppearanceSettingsRequestSchema.parse({
        theme: "dark",
        density: "compact",
        help_mode: "full",
      }),
    ).toEqual({ theme: "dark", density: "compact", help_mode: "full" });
  });

  it("rejects invalid values", () => {
    expect(() =>
      updateAppearanceSettingsRequestSchema.parse({
        theme: "neon",
        density: "compact",
        help_mode: "full",
      }),
    ).toThrow();
  });
});

describe("updatePanelSettingsRequestSchema", () => {
  it("accepts empty strings (cleared values)", () => {
    expect(
      updatePanelSettingsRequestSchema.parse({
        http_public_url: "",
        grpc_public_endpoint: "",
      }),
    ).toEqual({ http_public_url: "", grpc_public_endpoint: "" });
  });
});

describe("panelUpdateRequestSchema", () => {
  it("requires non-empty target_version", () => {
    expect(
      panelUpdateRequestSchema.parse({ target_version: "v1.2.3" }).target_version,
    ).toBe("v1.2.3");
    expect(() => panelUpdateRequestSchema.parse({ target_version: "" })).toThrow();
  });
});

describe("updateSettingsRequestSchema", () => {
  it("accepts all fields omitted", () => {
    expect(updateSettingsRequestSchema.parse({})).toEqual({});
  });

  it("accepts agent_download_source enum", () => {
    expect(
      updateSettingsRequestSchema.parse({ agent_download_source: "panel" })
        .agent_download_source,
    ).toBe("panel");
  });

  it("rejects unknown agent_download_source", () => {
    expect(() =>
      updateSettingsRequestSchema.parse({ agent_download_source: "s3" }),
    ).toThrow();
  });
});

describe("agentBootstrapRequestSchema", () => {
  it("requires node_name and version", () => {
    expect(
      agentBootstrapRequestSchema.parse({ node_name: "n", version: "v1" }),
    ).toEqual({ node_name: "n", version: "v1" });
    expect(() =>
      agentBootstrapRequestSchema.parse({ node_name: "", version: "v1" }),
    ).toThrow();
  });
});

describe("agentCertificateRecoveryRequestSchema", () => {
  it("requires all 5 proof fields", () => {
    expect(
      agentCertificateRecoveryRequestSchema.parse({
        agent_id: "a",
        certificate_pem: "-----BEGIN-----",
        proof_timestamp_unix: 1700000000,
        proof_nonce: "n",
        proof_signature: "s",
      }).agent_id,
    ).toBe("a");
    expect(() =>
      agentCertificateRecoveryRequestSchema.parse({
        agent_id: "a",
        certificate_pem: "-----BEGIN-----",
        proof_timestamp_unix: 0,
        proof_nonce: "n",
        proof_signature: "s",
      }),
    ).toThrow();
  });
});

describe("agentCertificateRecoveryGrantRequestSchema", () => {
  it("requires positive ttl_seconds", () => {
    expect(
      agentCertificateRecoveryGrantRequestSchema.parse({ ttl_seconds: 3600 }),
    ).toEqual({ ttl_seconds: 3600 });
    expect(() =>
      agentCertificateRecoveryGrantRequestSchema.parse({ ttl_seconds: 0 }),
    ).toThrow();
  });
});
