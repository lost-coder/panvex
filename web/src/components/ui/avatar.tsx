import { cn } from "@/lib/cn";

interface AvatarProps {
  name: string;
  size?: "default" | "sm";
}

export function Avatar({ name, size = "default" }: AvatarProps) {
  return (
    <div
      className={cn(
        "rounded-full bg-accent text-white font-bold flex items-center justify-center uppercase",
        size === "default" ? "w-8 h-8 text-xs" : "w-6 h-6 text-[10px]"
      )}
    >
      {name.charAt(0)}
    </div>
  );
}
