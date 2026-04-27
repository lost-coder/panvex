import { cn } from "@/ui/lib/cn";

export interface UserAvatarProps {
  name: string;
  size?: "sm" | "md" | "lg";
  online?: boolean;
  className?: string;
}

const sizeMap = {
  sm: "h-7 w-7 text-[10px]",
  md: "h-9 w-9 text-xs",
  lg: "h-11 w-11 text-sm",
} as const;

function initials(name: string): string {
  return name
    .split(/[\s_-]+/)
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase() ?? "")
    .join("");
}

const palette = [
  "bg-accent/20 text-accent",
  "bg-status-ok/20 text-status-ok",
  "bg-status-warn/20 text-status-warn",
  "bg-purple-500/20 text-purple-400",
  "bg-pink-500/20 text-pink-400",
  "bg-teal-500/20 text-teal-400",
];

function colorFromName(name: string): string {
  let hash = 0;
  for (let i = 0; i < name.length; i++) hash = (name.codePointAt(i) ?? 0) + ((hash << 5) - hash);
  // palette is non-empty (constant above), so modulo indexing always resolves.
  return palette[Math.abs(hash) % palette.length] ?? palette[0]!;
}

export function UserAvatar({ name, size = "md", online, className }: Readonly<UserAvatarProps>) {
  return (
    <div className={cn("relative inline-flex shrink-0", className)}>
      <span
        className={cn(
          "inline-flex items-center justify-center rounded-full font-mono font-semibold",
          sizeMap[size],
          colorFromName(name),
        )}
      >
        {initials(name)}
      </span>
      {online !== undefined && (
        <span
          className={cn(
            "absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border-2 border-bg",
            online ? "bg-status-ok" : "bg-fg-muted",
          )}
        />
      )}
    </div>
  );
}
