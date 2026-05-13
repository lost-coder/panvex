import type {
  EnrollmentAttemptDetail,
  EnrollmentEvent,
} from "@/shared/api/types-enrollment";

import { STEP_LABELS_RU } from "./enrollment-steps";

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

function colorClass(level: EnrollmentEvent["level"]): string {
  switch (level) {
    case "error":
      return "text-red-600 border-red-300";
    case "warn":
      return "text-amber-600 border-amber-300";
    default:
      return "text-emerald-600 border-emerald-300";
  }
}

// EnrollmentTimeline renders the ordered list of timeline events for a
// single enrollment attempt. Used both by the Add Server wizard (live
// view of the in-flight attempt) and by the Server Detail page
// (read-only history block). The component is stateless and takes the
// shape returned by GET /api/enrollment-attempts/{id} verbatim — the
// owning container is responsible for refetching / WS updates.
export function EnrollmentTimeline({ detail }: Props) {
  const { attempt, events } = detail;
  return (
    <div className="flex flex-col gap-3">
      {attempt.status === "failed" && attempt.error_message && (
        <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
          <div className="font-medium">Подключение не удалось</div>
          <div className="mt-1">{attempt.error_message}</div>
          {attempt.error_code && (
            <div className="mt-1 text-xs text-red-600">
              Код: {attempt.error_code}
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
              <div className="text-sm font-medium">
                {STEP_LABELS_RU[e.step] ?? e.step}
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
