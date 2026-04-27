import { cn } from "@/ui/lib/cn";

export interface SpinnerProps {
  size?: "sm" | "md" | "lg";
  className?: string;
}

const sizeMap = { sm: "h-4 w-4", md: "h-5 w-5", lg: "h-8 w-8" } as const;

export function Spinner({ size = "md", className }: Readonly<SpinnerProps>) {
  return (
    <output className="contents" aria-label="Loading">
      <svg
        className={cn("animate-spin text-accent", sizeMap[size], className)}
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden="true"
      >
        <circle className="opacity-20" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" />
        <path
          className="opacity-80"
          d="M12 2a10 10 0 0 1 10 10"
          stroke="currentColor"
          strokeWidth="3"
          strokeLinecap="round"
        />
      </svg>
    </output>
  );
}
