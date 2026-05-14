import { useTranslation } from "react-i18next";

import type {
  EnrollmentAttemptDetail,
  EnrollmentEvent,
} from "@/shared/api/types-enrollment";

import { stepLabelKey } from "./enrollment-steps";

interface Props {
  detail: EnrollmentAttemptDetail;
}

function iconFor(level: EnrollmentEvent["level"]): string {
  switch (level) {
    case "error":
      return "✕";
    case "warn":
      return "!";
    default:
      return "✓";
  }
}

// Use the project's semantic status tokens (status-ok / status-warn /
// status-error) rather than raw Tailwind palette classes so the dot +
// border stay legible in both light and dark themes. Tokens are defined
// in the design system and already account for contrast.
function colorClass(level: EnrollmentEvent["level"]): string {
  switch (level) {
    case "error":
      return "text-status-error border-status-error/40";
    case "warn":
      return "text-status-warn border-status-warn/40";
    default:
      return "text-status-ok border-status-ok/40";
  }
}

// EnrollmentTimeline renders the ordered list of timeline events for a
// single enrollment attempt. Used both by the Add Server wizard (live
// view of the in-flight attempt) and by the Server Detail page
// (read-only history block). The component is stateless and takes the
// shape returned by GET /api/enrollment-attempts/{id} verbatim — the
// owning container is responsible for refetching / WS updates.
export function EnrollmentTimeline({ detail }: Props) {
  const { t } = useTranslation("enrollment");
  const { attempt, events } = detail;
  return (
    <div className="flex flex-col gap-3">
      {attempt.status === "failed" && attempt.error_message && (
        <div className="rounded-md border border-status-error/30 bg-status-error/10 p-3 text-sm text-status-error">
          <div className="font-medium">{t("timeline.failed")}</div>
          <div className="mt-1">{attempt.error_message}</div>
          {attempt.error_code && (
            <div className="mt-1 text-xs text-status-error/80">
              {t("timeline.errorCode", { code: attempt.error_code })}
            </div>
          )}
        </div>
      )}
      <ol className="flex flex-col gap-2">
        {events.map((e, idx) => (
          <li
            // The backend does not assign per-event IDs; events are
            // append-only and ordered by ts, so the index plus the
            // timestamp gives a stable enough key for a small list.
            key={`${e.ts}-${idx}`}
            className="flex items-start gap-3"
          >
            <span
              className={`mt-0.5 inline-flex h-5 w-5 items-center justify-center rounded-full border ${colorClass(e.level)}`}
              aria-hidden="true"
            >
              {iconFor(e.level)}
            </span>
            <div className="flex flex-col">
              <div className="text-sm font-medium text-fg">
                {t(stepLabelKey(e.step), { defaultValue: e.step })}
              </div>
              {e.message && (
                <div className="text-xs text-fg-muted">{e.message}</div>
              )}
              <div className="text-xs text-fg-muted/70">
                {new Date(e.ts).toLocaleTimeString()}
              </div>
            </div>
          </li>
        ))}
      </ol>
    </div>
  );
}
