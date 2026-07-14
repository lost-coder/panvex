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

