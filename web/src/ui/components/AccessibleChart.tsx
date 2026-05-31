import { type ReactNode, useId } from "react";

/**
 * A single numeric series mirrored into the screen-reader data table.
 * `key` matches the recharts `dataKey`; `label` is the already-translated
 * (or human-readable) series name.
 */
export interface AccessibleChartSeries {
  key: string;
  label: string;
}

export interface AccessibleChartProps<T extends object> {
  /** Accessible title for the chart region (already translated). */
  title: string;
  /** Series rendered in the chart; used to build the hidden data table. */
  series: AccessibleChartSeries[];
  /** The same data array passed to the recharts chart. */
  data: readonly T[];
  /** Key in each datum used as the per-row label (e.g. the timestamp). */
  labelKey: keyof T;
  /** Column header for the label column (already translated). */
  labelHeader: string;
  /** Optional formatter for the row-label cell (e.g. format a timestamp). */
  formatLabel?: ((value: T[keyof T], row: T) => string) | undefined;
  /** Optional unit appended to the aria summary / numeric cells. */
  unit?: string | undefined;
  /** The recharts chart element to render visually. */
  children: ReactNode;
  className?: string | undefined;
}

function toNumber(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/**
 * U1 / P2-FE: recharts SVG charts are invisible to assistive tech and
 * keyboard users. This wrapper adds two affordances around any chart:
 *
 * 1. the visual chart is exposed as `role="img"` with a one-line
 *    `aria-label` summary (current / min / max of the first series), and
 * 2. a visually-hidden (`sr-only`) `<table>` mirrors every numeric series
 *    so a screen reader can read the underlying values cell by cell.
 *
 * Callers keep full control of the recharts markup — they pass it as
 * children — so this stays chart-type agnostic (area/line/bar/sparkline).
 */
export function AccessibleChart<T extends object>({
  title,
  series,
  data,
  labelKey,
  labelHeader,
  formatLabel,
  unit,
  children,
  className,
}: Readonly<AccessibleChartProps<T>>) {
  const tableId = useId();
  const unitSuffix = unit ?? "";

  // One-line summary from the first series: current / min / max.
  let summary = title;
  const first = series[0];
  if (first && data.length > 0) {
    const values = data
      .map((row) => toNumber(row[first.key as keyof T]))
      .filter((v): v is number => v !== null);
    if (values.length > 0) {
      const current = values[values.length - 1];
      const min = Math.min(...values);
      const max = Math.max(...values);
      summary =
        `${title}. ${first.label}: ` +
        `current ${current}${unitSuffix}, min ${min}${unitSuffix}, max ${max}${unitSuffix}.`;
    }
  }

  const renderLabel = (row: T): string => {
    const raw = row[labelKey];
    if (formatLabel) return formatLabel(raw, row);
    return String(raw);
  };

  return (
    <div className={className}>
      <div role="img" aria-label={summary} className="h-full w-full">
        {children}
      </div>
      <table className="sr-only" id={tableId}>
        <caption>{title}</caption>
        <thead>
          <tr>
            <th scope="col">{labelHeader}</th>
            {series.map((s) => (
              <th key={s.key} scope="col">
                {unitSuffix.trim() ? `${s.label} (${unitSuffix.trim()})` : s.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((row, rowIndex) => (
            <tr key={`${renderLabel(row)}-${rowIndex}`}>
              <th scope="row">{renderLabel(row)}</th>
              {series.map((s) => {
                const v = toNumber(row[s.key as keyof T]);
                return (
                  <td key={s.key}>{v === null ? "" : `${v}${unitSuffix}`}</td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
