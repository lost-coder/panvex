// Translation-key helpers for the enrollment timeline. The step ids
// match enrollment.Step constants in internal/controlplane/enrollment;
// the resolved label renders in the dashboard's EnrollmentTimeline when
// the backend publishes a matching `enrollment.event`.
//
// Strings themselves live in src/locales/{en,ru}/enrollment.json under
// the `step.<id>` keys. Unknown step keys fall back to the raw machine
// token at render time (via the `defaultValue` option on t()), so this
// helper can stay frozen while the backend adds new steps without
// blocking the UI.

/**
 * Build the i18n key for a given backend step id. Used together with
 * `t(stepLabelKey(step), { defaultValue: step })` so unrecognised step
 * tokens fall back to the raw id rather than rendering an i18n key.
 */
export function stepLabelKey(step: string): string {
  return `step.${step}`;
}

/**
 * Known step ids, kept here so engineers grep'ing for "what steps does
 * the timeline render?" land on one canonical list. The set is not used
 * at render time — components rely on the defaultValue fallback above —
 * but tests and dev tooling import it.
 */
export const KNOWN_ENROLLMENT_STEPS = [
  "bootstrap_request_received",
  "token_validated",
  "csr_received",
  "csr_validated",
  "cert_signed",
  "cert_returned",
  "agent_persisted_cert",
  "gateway_dialed",
  "tls_handshake_ok",
  "first_sync_ok",
  "install_command_issued",
  "outbound_listener_started",
  "panel_dial_attempted",
] as const;
