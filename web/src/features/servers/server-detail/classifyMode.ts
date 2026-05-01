import type { ModeKind } from "@/shared/api/types-pages/pages";

interface ClassifyInput {
  useMiddleProxy: boolean;
  meRuntimeReady: boolean;
  me2dcFallbackEnabled: boolean;
}

export function classifyMode(input: ClassifyInput): ModeKind {
  if (!input.useMiddleProxy) return "direct";
  if (input.meRuntimeReady) return "me";
  if (input.me2dcFallbackEnabled) return "fallback";
  return "me_down";
}
