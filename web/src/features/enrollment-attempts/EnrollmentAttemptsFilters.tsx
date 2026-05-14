import { useTranslation } from "react-i18next";

import type {
  EnrollmentAttemptsFilter,
  EnrollmentMode,
  EnrollmentStatus,
} from "@/shared/api/types-enrollment";

interface Props {
  value: EnrollmentAttemptsFilter;
  onChange: (next: EnrollmentAttemptsFilter) => void;
  onReset: () => void;
}

const STATUS_OPTIONS: EnrollmentStatus[] = ["in_progress", "success", "failed"];
const MODE_OPTIONS: EnrollmentMode[] = ["inbound", "outbound"];

// Mirrors the canonical EnrollmentErrorCode set declared on the Go side
// in internal/controlplane/enrollment/errorcodes.go. Keeping the list
// hard-coded (rather than fetching it) avoids a roundtrip on first
// paint and the set is short and rarely changes — when a new code is
// added the Phase-3 §3.b checklist asks the implementer to update both
// halves together.
const ERROR_CODES = [
  "TOKEN_EXPIRED",
  "TOKEN_ALREADY_USED",
  "TOKEN_NOT_FOUND",
  "TLS_PIN_MISMATCH",
  "PANEL_UNREACHABLE",
  "CSR_INVALID",
  "CSR_SUBJECT_MISMATCH",
  "CERT_SIGN_FAILED",
  "OUTBOUND_DIAL_TIMEOUT",
  "OUTBOUND_LISTENER_REFUSED",
  "INTERNAL_ERROR",
];

// EnrollmentAttemptsFilters renders the filter chrome above the
// attempts table. Each change clears the pagination cursor so the
// next list call starts from page 0 — keeping a stale cursor across
// filter edits would re-anchor the cursor against rows that no
// longer match the active filter set.
export function EnrollmentAttemptsFilters({ value, onChange, onReset }: Readonly<Props>) {
  const { t } = useTranslation("enrollment-attempts");

  return (
    <div className="flex flex-wrap items-end gap-3">
      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.status")}
        <select
          value={value.status ?? ""}
          onChange={(e) =>
            onChange({
              ...value,
              status: (e.target.value || undefined) as EnrollmentStatus | undefined,
              cursor: undefined,
            })
          }
          className="rounded-md border border-divider bg-bg-card px-2 py-1 text-sm text-fg"
        >
          <option value="">{t("filters.anyStatus")}</option>
          {STATUS_OPTIONS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.mode")}
        <select
          value={value.mode ?? ""}
          onChange={(e) =>
            onChange({
              ...value,
              mode: (e.target.value || undefined) as EnrollmentMode | undefined,
              cursor: undefined,
            })
          }
          className="rounded-md border border-divider bg-bg-card px-2 py-1 text-sm text-fg"
        >
          <option value="">{t("filters.anyMode")}</option>
          {MODE_OPTIONS.map((m) => (
            <option key={m} value={m}>
              {m}
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.errorCode")}
        <select
          value={value.error_code ?? ""}
          onChange={(e) =>
            onChange({
              ...value,
              error_code: e.target.value || undefined,
              cursor: undefined,
            })
          }
          className="rounded-md border border-divider bg-bg-card px-2 py-1 text-sm text-fg"
        >
          <option value="">{t("filters.anyErrorCode")}</option>
          {ERROR_CODES.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.startedAfter")}
        <input
          type="date"
          value={value.started_after?.slice(0, 10) ?? ""}
          onChange={(e) =>
            onChange({
              ...value,
              started_after: e.target.value
                ? new Date(e.target.value).toISOString()
                : undefined,
              cursor: undefined,
            })
          }
          className="rounded-md border border-divider bg-bg-card px-2 py-1 text-sm text-fg"
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.startedBefore")}
        <input
          type="date"
          value={value.started_before?.slice(0, 10) ?? ""}
          onChange={(e) =>
            onChange({
              ...value,
              started_before: e.target.value
                ? new Date(e.target.value).toISOString()
                : undefined,
              cursor: undefined,
            })
          }
          className="rounded-md border border-divider bg-bg-card px-2 py-1 text-sm text-fg"
        />
      </label>

      <button
        type="button"
        onClick={onReset}
        className="rounded-md border border-divider px-3 py-1 text-sm text-fg-muted hover:text-fg"
      >
        {t("filters.reset")}
      </button>
    </div>
  );
}
