import { useEffect, useRef } from "react";

/**
 * Generic keyboard-shortcut primitive. Registers a document-level keydown
 * listener that fires `handler` when the user types the given sequence.
 *
 * Supports two forms:
 *   • single key: `"?"` or `"Escape"` — matches the KeyboardEvent.key
 *   • two-key leader sequence: `"g s"` — matches leader then follow-up
 *     within `timeoutMs` (vim-style navigation a-la Linear/GitHub).
 *
 * The listener is skipped whenever focus is inside an input/textarea/
 * contenteditable so typing into a search field never teleports the
 * user to another route.
 */
export function useKeyboardShortcut(
  shortcut: string,
  handler: (event: KeyboardEvent) => void,
  options: { timeoutMs?: number; enabled?: boolean } = {},
) {
  const { timeoutMs = 800, enabled = true } = options;
  const handlerRef = useRef(handler);
  // Writing to a ref during render violates react-hooks/refs. An effect
  // keeps the ref in sync with the latest handler without re-subscribing
  // the document listener on every render.
  useEffect(() => {
    handlerRef.current = handler;
  }, [handler]);

  useEffect(() => {
    if (!enabled || typeof document === "undefined") return;

    const parts = shortcut.trim().split(/\s+/);
    let leaderActive = false;
    let leaderTimer: ReturnType<typeof setTimeout> | null = null;

    const isEditable = (target: EventTarget | null): boolean => {
      if (!(target instanceof HTMLElement)) return false;
      const tag = target.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
      if (target.isContentEditable) return true;
      return false;
    };

    const clearLeader = () => {
      leaderActive = false;
      if (leaderTimer) {
        clearTimeout(leaderTimer);
        leaderTimer = null;
      }
    };

    const onKeyDown = (event: KeyboardEvent) => {
      if (isEditable(event.target)) return;
      // Modifier keys inflate the chord; we only support plain keys for now.
      if (event.metaKey || event.ctrlKey || event.altKey) return;

      if (parts.length === 1) {
        if (event.key === parts[0]) {
          handlerRef.current(event);
        }
        return;
      }

      // Two-key leader sequence.
      const [leader, follow] = parts;
      if (!leaderActive) {
        if (event.key === leader) {
          leaderActive = true;
          leaderTimer = setTimeout(clearLeader, timeoutMs);
        }
        return;
      }
      if (event.key === follow) {
        clearLeader();
        handlerRef.current(event);
        return;
      }
      // Any other key aborts the sequence so Shift etc. doesn't trap it.
      clearLeader();
    };

    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      if (leaderTimer) clearTimeout(leaderTimer);
    };
  }, [shortcut, timeoutMs, enabled]);
}
