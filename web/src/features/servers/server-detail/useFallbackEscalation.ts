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
 * fires exactly at T+30min and writes a fresh "now" into state, forcing
 * the memo to recompute. The timeout is a one-shot per fallback window,
 * so it does not keep firing once escalated.
 *
 * Sprint S26 tail: react-hooks/purity flagged the original implementation
 * for reading Date.now() inside the useMemo body — the rule only allows
 * impure calls inside effects / state initialisers / event handlers. We
 * now hold the current "now in seconds" in state, initialise it lazily
 * (Date.now() in the initialiser is allowed), and the same effect that
 * schedules the escalation boundary also refreshes it via setNowSeconds.
 */
export function useFallbackEscalation(
  mode: ModeKind,
  enteredAtUnix: number | null | undefined,
): FallbackEscalationState {
  // Lazy initialiser: Date.now() inside `useState(() => …)` runs once at
  // mount and is exempt from react-hooks/purity. Subsequent updates land
  // through the setTimeout in the effect below.
  const [nowSeconds, setNowSeconds] = useState(() => Math.floor(Date.now() / 1000));

  const normalizedEnteredAtUnix =
    typeof enteredAtUnix === "number" && Number.isFinite(enteredAtUnix)
      ? enteredAtUnix
      : null;

  // Pure projection over `nowSeconds` + the inputs — no Date.now() reads
  // during render, which keeps react-hooks/purity happy. Tests still
  // control time via vi.useFakeTimers() / vi.setSystemTime() because the
  // effect below re-reads Date.now() under fake timers just like before.
  const state = useMemo<FallbackEscalationState>(() => {
    if (mode !== "fallback") {
      return { active: false, durationSeconds: 0, escalated: false, enteredAtUnix: null };
    }
    const baseSeconds = normalizedEnteredAtUnix ?? nowSeconds;
    const durationSeconds = Math.max(0, nowSeconds - baseSeconds);
    return {
      active: true,
      durationSeconds,
      escalated: durationSeconds >= ESCALATION_THRESHOLD_SECONDS,
      enteredAtUnix: normalizedEnteredAtUnix,
    };
  }, [mode, normalizedEnteredAtUnix, nowSeconds]);

  useEffect(() => {
    if (mode !== "fallback") return;
    if (normalizedEnteredAtUnix == null) return;
    if (state.escalated) return;

    const elapsedMs = Date.now() - normalizedEnteredAtUnix * 1000;
    const remainingMs = ESCALATION_THRESHOLD_SECONDS * 1000 - elapsedMs;
    const refreshNow = () => setNowSeconds(Math.floor(Date.now() / 1000));

    if (remainingMs <= 0) {
      // Already past the boundary but escalated is false (e.g. clock jump
      // across the threshold) — schedule a microtask so we re-derive
      // without re-entering the effect synchronously.
      const id = window.setTimeout(refreshNow, 0);
      return () => window.clearTimeout(id);
    }

    const id = window.setTimeout(refreshNow, remainingMs);
    return () => window.clearTimeout(id);
  }, [mode, normalizedEnteredAtUnix, state.escalated]);

  return state;
}
