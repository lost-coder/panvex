import { cn } from "@/ui/lib/cn";

export interface MiniChartProps {
  data: number[];
  width?: number;
  height?: number;
  color?: string;
  className?: string;
}

export function MiniChart({
  data,
  width = 80,
  height = 24,
  color = "#60a5fa",
  className,
}: Readonly<MiniChartProps>) {
  if (data.length < 2) return null;

  const min = Math.min(...data);
  const max = Math.max(...data);
  const range = max - min || 1;
  const padding = 1;

  const points = data.map((v, i) => {
    const x = padding + (i / (data.length - 1)) * (width - padding * 2);
    const y = height - padding - ((v - min) / range) * (height - padding * 2);
    return `${x},${y}`;
  });

  const areaPoints = [
    `${padding},${height - padding}`,
    ...points,
    `${width - padding},${height - padding}`,
  ];

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={cn("shrink-0", className)}
    >
      <polygon points={areaPoints.join(" ")} fill={color} fillOpacity={0.1} />
      <polyline
        points={points.join(" ")}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
