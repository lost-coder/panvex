import { z } from "zod";

// 3.14: PUT /settings/values accepts a flat map of registry setting names
// (see internal/controlplane/settings' schema/values registry — arbitrary
// `name -> value` pairs, keyed dynamically by the bootstrap/operational
// schema entries) to their new scalar value. The key set isn't fixed at
// compile time — z.record captures that shape while still rejecting
// non-scalar values (arrays/objects/null) the API can't accept.
export const settingsValuesRequestSchema = z.record(
  z.string().min(1),
  z.union([z.string(), z.number(), z.boolean()]),
);

export type SettingsValuesRequest = z.infer<typeof settingsValuesRequestSchema>;
