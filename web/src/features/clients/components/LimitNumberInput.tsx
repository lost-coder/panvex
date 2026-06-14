import { useState } from "react";

import { Input } from "@/ui/base/input";

interface LimitNumberInputProps {
  /** Current committed value; 0 = unlimited. */
  value: number;
  onValueChange: (value: number) => void;
  placeholder?: string | undefined;
  disabled?: boolean | undefined;
  ariaLabel?: string | undefined;
}

/**
 * Non-negative integer input for the limits block (audit E3). Local
 * draft so a transiently-empty field doesn't commit 0 (= unlimited);
 * empty or negative blur restores the previous value — clearing a limit
 * requires explicitly typing 0.
 */
export function LimitNumberInput({
  value,
  onValueChange,
  placeholder,
  disabled,
  ariaLabel,
}: Readonly<LimitNumberInputProps>) {
  const [draft, setDraft] = useState(value === 0 ? "" : String(value));

  const commit = () => {
    if (draft.trim() === "") {
      setDraft(value === 0 ? "" : String(value));
      return;
    }
    const parsed = Math.floor(Number(draft));
    if (!Number.isFinite(parsed) || parsed < 0) {
      setDraft(value === 0 ? "" : String(value));
      return;
    }
    onValueChange(parsed);
    setDraft(parsed === 0 ? "" : String(parsed));
  };

  return (
    <Input
      type="number"
      min={0}
      step={1}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      placeholder={placeholder}
      className="font-mono text-xs"
      disabled={disabled}
      aria-label={ariaLabel}
    />
  );
}
