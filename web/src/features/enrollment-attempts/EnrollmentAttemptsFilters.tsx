import { useTranslation } from "react-i18next";

import { Button, Input, Select } from "@/ui";
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

  // "all" is the cleared sentinel: ui/base/Select only renders a placeholder
  // when the value is empty, so a truthy sentinel keeps the "Any …" option
  // reselectable (mapped back to undefined on change).
  return (
    <div className="flex flex-wrap items-end gap-3">
      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.status")}
        <Select
          value={value.status ?? "all"}
          onChange={(v) =>
            onChange({
              ...value,
              status: v === "all" ? undefined : (v as EnrollmentStatus),
              cursor: undefined,
            })
          }
          options={[
            { value: "all", label: t("filters.anyStatus") },
            ...STATUS_OPTIONS.map((s) => ({ value: s, label: s })),
          ]}
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.mode")}
        <Select
          value={value.mode ?? "all"}
          onChange={(v) =>
            onChange({
              ...value,
              mode: v === "all" ? undefined : (v as EnrollmentMode),
              cursor: undefined,
            })
          }
          options={[
            { value: "all", label: t("filters.anyMode") },
            ...MODE_OPTIONS.map((m) => ({ value: m, label: m })),
          ]}
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.errorCode")}
        <Select
          value={value.error_code ?? "all"}
          onChange={(v) =>
            onChange({
              ...value,
              error_code: v === "all" ? undefined : v,
              cursor: undefined,
            })
          }
          options={[
            { value: "all", label: t("filters.anyErrorCode") },
            ...ERROR_CODES.map((c) => ({ value: c, label: c })),
          ]}
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.startedAfter")}
        <Input
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
        />
      </label>

      <label className="flex flex-col gap-1 text-xs text-fg-muted">
        {t("filters.startedBefore")}
        <Input
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
        />
      </label>

      <Button type="button" variant="ghost" size="sm" onClick={onReset}>
        {t("filters.reset")}
      </Button>
    </div>
  );
}
