import { useEffect, useMemo, useState } from "react";

import type { ModeKind } from "@/shared/api/types-pages/pages";

const ESCALATION_THRESHOLD_SECONDS = 30 * 60;

export interface FallbackEscalationState {
  active: boolean;
  durationSeconds: number;
  escalated: boolean;
  enteredAtUnix: number | null;
}

/**
 * Computes the fallback banner state from the panel's classified mode and
 * the agent-reported `fallback_entered_at_unix` timestamp.
 *
 * The 30-minute escalation boundary is wall-clock based, not data-driven:
 * if a node enters fallback at T and the page sits idle for 35 min without
 * a re-fetch, the badge would otherwise stay at "warn" until the next
 * server response. To close that gap we schedule a single setTimeout that
 * fires exactly at T+30min and bumps a tick state, forcing the memo to
 * recompute. The timeout is a one-shot per fallback window, so it does not
 * keep firing once escalated.
 */
export function useFallbackEscalation(
  mode: ModeKind,
  enteredAtUnix: number | null | undefined,
): FallbackEscalationState {
  const [tick, setTick] = useState(0);

  const normalizedEnteredAtUnix =
    typeof enteredAtUnix === "number" && Number.isFinite(enteredAtUnix)
      ? enteredAtUnix
      : null;

  // Re-derive on tick bumps, mode changes, and fallback timestamp updates.
  // We deliberately read Date.now() here rather than threading a clock arg
  // — the hook is a thin client-side projection and consumers test it via
  // vi.useFakeTimers() / vi.setSystemTime().
  const state = useMemo<FallbackEscalationState>(() => {
    if (mode !== "fallback") {
      return { active: false, durationSeconds: 0, escalated: false, enteredAtUnix: null };
    }
    const baseSeconds = normalizedEnteredAtUnix ?? Math.floor(Date.now() / 1000);
    const durationSeconds = Math.max(0, Math.floor(Date.now() / 1000) - baseSeconds);
    return {
      active: true,
      durationSeconds,
      escalated: durationSeconds >= ESCALATION_THRESHOLD_SECONDS,
      enteredAtUnix: normalizedEnteredAtUnix,
    };
    // tick is intentional: bumping it forces this memo to re-read Date.now()
    // when the escalation boundary fires.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, normalizedEnteredAtUnix, tick]);

  useEffect(() => {
    if (mode !== "fallback") return;
    if (normalizedEnteredAtUnix == null) return;
    if (state.escalated) return;

    const elapsedMs = Date.now() - normalizedEnteredAtUnix * 1000;
    const remainingMs = ESCALATION_THRESHOLD_SECONDS * 1000 - elapsedMs;
    if (remainingMs <= 0) {
      // Already past the boundary but escalated is false (e.g. clock jump
      // across the threshold) — schedule a microtask so we re-derive
      // without re-entering the effect synchronously.
      const id = window.setTimeout(() => setTick((value) => value + 1), 0);
      return () => window.clearTimeout(id);
    }

    const id = window.setTimeout(
      () => setTick((value) => value + 1),
      remainingMs,
    );
    return () => window.clearTimeout(id);
  }, [mode, normalizedEnteredAtUnix, state.escalated]);

  return state;
}
