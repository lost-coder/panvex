import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";
import type { ZodType } from "zod";

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

const here = dirname(fileURLToPath(import.meta.url));
// Fixture lives in the Go module: core/internal/controlplane/server/testdata/api/requests.json.
// Up from web/src/shared/api/schemas/requests/: 6 levels to reach core/, then descend into the Go testdata.
const fixturePath = resolve(
  here,
  "../../../../../../internal/controlplane/server/testdata/api/requests.json",
);

type FixtureBundle = Record<string, Record<string, unknown>>;

const bundle = JSON.parse(readFileSync(fixturePath, "utf8")) as FixtureBundle;

const schemas: Record<string, ZodType<unknown>> = {
  loginRequest: loginRequestSchema,
  updateTotpRequest: updateTotpRequestSchema,
  createUserRequest: createUserRequestSchema,
  updateUserRequest: updateUserRequestSchema,
  clientMutationRequest: clientMutationRequestSchema,
  renameAgentRequest: renameAgentRequestSchema,
  createEnrollmentTokenRequest: createEnrollmentTokenRequestSchema,
  createJobRequest: createJobRequestSchema,
  updateAppearanceSettingsRequest: updateAppearanceSettingsRequestSchema,
  updatePanelSettingsRequest: updatePanelSettingsRequestSchema,
  panelUpdateRequest: panelUpdateRequestSchema,
  updateSettingsRequest: updateSettingsRequestSchema,
  agentBootstrapRequest: agentBootstrapRequestSchema,
  agentCertificateRecoveryRequest: agentCertificateRecoveryRequestSchema,
  agentCertificateRecoveryGrantRequest: agentCertificateRecoveryGrantRequestSchema,
};

describe("request schema contract — Go fixtures ↔ Zod", () => {
  it("fixture bundle covers every request schema", () => {
    const missing = Object.keys(schemas).filter((key) => !bundle[key]);
    expect(missing).toEqual([]);
  });

  it("every schema has at least one variant in the fixture bundle", () => {
    const empty = Object.keys(schemas).filter(
      (key) => Object.keys(bundle[key] ?? {}).length === 0,
    );
    expect(empty).toEqual([]);
  });

  for (const [name, schema] of Object.entries(schemas)) {
    const variants = bundle[name];
    if (!variants) continue;
    for (const [variantName, payload] of Object.entries(variants)) {
      it(`${name}/${variantName} parses cleanly`, () => {
        const result = schema.safeParse(payload);
        const detail = result.success
          ? ""
          : `Zod rejected fixture ${name}/${variantName}:\n${JSON.stringify(result.error.issues, null, 2)}`;
        expect(result.success, detail).toBe(true);
      });
    }
  }
});
