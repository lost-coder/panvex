import React from "react";

type BreakdownTone = "neutral" | "good" | "warn" | "bad";

export type DashboardSummaryCardBreakdownItem = {
  label: string;
  value: string | number;
  tone?: BreakdownTone;
};

export type DashboardSummaryCardProps = {
  label: string;
  value: string | number;
  secondaryText?: string;
  breakdownItems?: DashboardSummaryCardBreakdownItem[];
};

function toneClassName(tone: BreakdownTone | undefined): string {
  if (tone === "good") {
    return "text-good-text";
  }

  if (tone === "warn") {
    return "text-warn-text";
  }

  if (tone === "bad") {
    return "text-bad-text";
  }

  return "text-text-2";
}

export function DashboardSummaryCard({
  label,
  value,
  secondaryText,
  breakdownItems,
}: DashboardSummaryCardProps) {
  return (
    <section className="bg-card border border-border rounded p-4 backdrop-blur-[var(--blur)]">
      <p className="text-[10px] font-bold uppercase tracking-[0.1em] text-text-3">
        {label}
      </p>
      <p className="mt-1 text-2xl font-extrabold tracking-tight text-text-1">{value}</p>
      {secondaryText ? (
        <p className="mt-1 text-xs font-semibold text-text-2">{secondaryText}</p>
      ) : null}
      {breakdownItems && breakdownItems.length > 0 ? (
        <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] font-semibold">
          {breakdownItems.map((item) => (
            <span key={item.label} className={toneClassName(item.tone)}>
              {item.label}: {item.value}
            </span>
          ))}
        </div>
      ) : null}
    </section>
  );
}
