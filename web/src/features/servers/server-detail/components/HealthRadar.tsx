import type { ServerDcData } from "@/shared/api/types-pages/pages";

// ─── Health radar (12 DCs as a circular dial) ─────────────────────────
// A 12-segment pie-ring coloured by DC status plus a hub that reports
// fleet-average coverage. Clicking a segment opens the DC detail sheet.
export function HealthRadar({
  dcs,
  onSelect,
}: {
  dcs: ServerDcData[];
  onSelect: (dc: ServerDcData) => void;
}) {
  const size = 240;
  const cx = size / 2;
  const cy = size / 2;
  const outer = size / 2 - 10;
  const inner = outer - 30;
  const gap = 2;

  // Order DCs by their dc number so the dial layout is stable across
  // renders. MTProto exposes positive + negative DC ids (e.g. -1..-6 for
  // media mirrors and 1..6 for the primary DCs); sort puts negatives
  // first, giving a consistent clockwise walk. No padding / slot-filling
  // is done — we render as many arcs as `dcs` contains, which previously
  // caused duplicates when a find-by-dc-number fallback substituted
  // `dcs[i]` with the wrong record.
  const ordered = [...dcs].sort((a, b) => a.dc - b.dc);
  const segCount = Math.max(1, ordered.length);
  const each = 360 / segCount;

  const arcPath = (i: number) => {
    const startA = -90 + i * each + gap / 2;
    const endA = -90 + (i + 1) * each - gap / 2;
    const rad = (a: number) => (a * Math.PI) / 180;
    const p = (r: number, a: number) => [cx + r * Math.cos(rad(a)), cy + r * Math.sin(rad(a))];
    const [x1, y1] = p(outer, startA);
    const [x2, y2] = p(outer, endA);
    const [x3, y3] = p(inner, endA);
    const [x4, y4] = p(inner, startA);
    return `M ${x1} ${y1} A ${outer} ${outer} 0 0 1 ${x2} ${y2} L ${x3} ${y3} A ${inner} ${inner} 0 0 0 ${x4} ${y4} Z`;
  };
  const labelPos = (i: number) => {
    const a = ((-90 + (i + 0.5) * each) * Math.PI) / 180;
    const r = (outer + inner) / 2;
    return [cx + r * Math.cos(a), cy + r * Math.sin(a)] as const;
  };

  const statusOf = (d: ServerDcData): "ok" | "warn" | "error" =>
    d.coveragePct < 70 ? "error" : d.coveragePct < 100 ? "warn" : "ok";
  const fillOf = (d: ServerDcData): string => {
    const s = statusOf(d);
    return s === "error"
      ? "var(--color-status-error)"
      : s === "warn"
        ? "var(--color-status-warn)"
        : "var(--color-status-ok)";
  };

  const total = ordered.length || 1;
  const avgPct = Math.round(
    ordered.reduce((sum, d) => sum + (d.coveragePct ?? 0), 0) / total,
  );
  const okCount = ordered.filter((d) => statusOf(d) === "ok").length;

  return (
    <div className="flex items-center justify-center">
      <svg viewBox={`0 0 ${size} ${size}`} width="100%" style={{ maxWidth: size }}>
        <circle
          cx={cx}
          cy={cy}
          r={outer + 3}
          fill="none"
          stroke="var(--color-divider)"
          strokeDasharray="1 3"
        />
        {ordered.map((d, i) => (
          <g key={d.dc} onClick={() => onSelect(d)} style={{ cursor: "pointer" }}>
            <path d={arcPath(i)} fill={fillOf(d)} opacity={statusOf(d) === "ok" ? 0.85 : 1} />
            {/* Mono numeral centred inside the arc. Keeps negative DC
                ids like "-4" legible next to positives. */}
            <text
              x={labelPos(i)[0]}
              y={labelPos(i)[1]}
              textAnchor="middle"
              dominantBaseline="middle"
              fontSize="9"
              fontFamily="JetBrains Mono, monospace"
              fontWeight={700}
              fill="rgba(11,13,18,0.9)"
              style={{ pointerEvents: "none", userSelect: "none" }}
            >
              {d.dc}
            </text>
          </g>
        ))}
        <circle
          cx={cx}
          cy={cy}
          r={inner - 8}
          fill="var(--color-bg)"
          stroke="var(--color-divider)"
        />
        <text
          x={cx}
          y={cy - 4}
          textAnchor="middle"
          fontSize="24"
          fontFamily="JetBrains Mono, monospace"
          fontWeight={600}
          fill="var(--color-fg)"
        >
          {avgPct}%
        </text>
        <text
          x={cx}
          y={cy + 12}
          textAnchor="middle"
          fontSize="9"
          fontFamily="JetBrains Mono, monospace"
          fill="var(--color-fg-muted)"
          letterSpacing="1"
        >
          {okCount}/{total} NOMINAL
        </text>
      </svg>
    </div>
  );
}
