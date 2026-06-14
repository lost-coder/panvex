import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Input } from "@/ui/base/input";
import {
  QUOTA_UNITS,
  type QuotaUnit,
  displayToQuota,
  quotaToDisplay,
} from "../quota-units";

interface QuotaInputProps {
  /** Quota in bytes; 0 = unlimited. */
  bytes: number;
  onBytesChange: (bytes: number) => void;
  disabled?: boolean | undefined;
}

/**
 * Value + unit editor for the data quota (audit E3). Form state stays in
 * raw bytes — the API payload is unchanged. A local draft string keeps a
 * transiently-empty input from collapsing to 0 (= unlimited, silently
 * removing the quota); empty/invalid blur restores the previous value.
 * An untouched non-round byte value is never re-rounded — conversion
 * happens only on a user edit.
 */
export function QuotaInput({ bytes, onBytesChange, disabled }: Readonly<QuotaInputProps>) {
  const { t } = useTranslation("clients");
  const initial = quotaToDisplay(bytes);
  const [draft, setDraft] = useState(initial.value === 0 ? "" : String(initial.value));
  const [unit, setUnit] = useState<QuotaUnit>(initial.unit);

  const restore = () => {
    const prev = quotaToDisplay(bytes);
    setDraft(prev.value === 0 ? "" : String(prev.value));
    setUnit(prev.unit);
  };

  const commit = (nextDraft: string, nextUnit: QuotaUnit) => {
    if (nextDraft.trim() === "") {
      restore();
      return;
    }
    const parsed = Number(nextDraft);
    if (!Number.isFinite(parsed) || parsed < 0) {
      restore();
      return;
    }
    onBytesChange(displayToQuota(parsed, nextUnit));
    setDraft(parsed === 0 ? "" : String(parsed));
  };

  return (
    <div className="flex gap-1.5">
      <Input
        type="number"
        min={0}
        step="any"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => commit(draft, unit)}
        placeholder={t("form.unlimitedPlaceholder")}
        className="font-mono text-xs flex-1 min-w-0"
        disabled={disabled}
        aria-label={t("form.dataQuotaLabel")}
      />
      <select
        value={unit}
        onChange={(e) => {
          const nextUnit = e.target.value as QuotaUnit;
          setUnit(nextUnit);
          if (draft.trim() !== "") commit(draft, nextUnit);
        }}
        disabled={disabled}
        aria-label={t("form.quotaUnitLabel")}
        className="rounded-xs border border-border-hi bg-bg-card px-2 text-xs font-mono text-fg"
      >
        {QUOTA_UNITS.map((u) => (
          <option key={u} value={u}>
            {t(`form.quotaUnits.${u}`)}
          </option>
        ))}
      </select>
    </div>
  );
}
