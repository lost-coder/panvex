import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { cn, type ToastVariant } from "@lost-coder/panvex-ui";

// Public API for app code. Consumers call useToast() and receive ToastAPI.
// Each push returns the toast id so callers can imperatively dismiss (e.g.
// dismiss a "saving…" toast once the mutation resolves).
export interface ToastAPI {
  success(message: string, opts?: { duration?: number }): number;
  error(message: string, opts?: { duration?: number }): number;
  info(message: string, opts?: { duration?: number }): number;
  dismiss(id: number): void;
}

interface ToastEntry {
  id: number;
  message: string;
  variant: ToastVariant;
  duration: number;
}

// Default auto-dismiss matches the remediation plan (P2-FE-03 spec: 5s).
// Errors linger slightly longer so operators have time to read and screenshot.
const DEFAULT_DURATION = 5000;
const ERROR_DURATION = 7000;

// Cap visible toasts. Stacking many would push the top one off-screen and
// on mobile the viewport is already tight. When the cap is hit we drop the
// oldest toast (FIFO); newer messages tend to be more actionable.
const MAX_VISIBLE = 3;

const ToastContext = createContext<ToastAPI | null>(null);

const variantBorder: Record<ToastVariant, string> = {
  success: "border-l-status-ok",
  error: "border-l-status-error",
  info: "border-l-accent",
};

const variantIconColor: Record<ToastVariant, string> = {
  success: "text-status-ok",
  error: "text-status-error",
  info: "text-accent",
};

const variantIcon: Record<ToastVariant, string> = {
  success: "✓",
  error: "✕",
  info: "ℹ",
};

/**
 * StackedToast is a local rendering of a single toast. We don't reuse the
 * UI-kit `<Toast>` primitive directly because it is position:fixed to the
 * viewport, which means multiple instances would overlap. Here we render
 * each entry as a flex child inside a single fixed viewport container
 * (`ToastViewport`), so the browser lays them out vertically for us.
 *
 * Visuals match the UI-kit Toast (border-left accent, icon, message) so
 * the look is consistent with Storybook — this is purely a layout wrapper.
 */
function StackedToast({
  entry,
  onClose,
}: {
  entry: ToastEntry;
  onClose: () => void;
}) {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    // Mount -> animate in on next frame.
    const mount = window.setTimeout(() => setVisible(true), 10);
    const hide = window.setTimeout(() => setVisible(false), entry.duration);
    const close = window.setTimeout(onClose, entry.duration + 200);
    return () => {
      window.clearTimeout(mount);
      window.clearTimeout(hide);
      window.clearTimeout(close);
    };
  }, [entry.duration, onClose]);

  return (
    <div
      className={cn(
        "pointer-events-auto flex items-center gap-2 rounded-xs bg-bg-card border border-border-hi border-l-[3px] pl-4 pr-2 py-3 shadow-xl",
        "transition-all duration-200",
        variantBorder[entry.variant],
        visible ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2",
      )}
      role={entry.variant === "error" ? "alert" : "status"}
    >
      <span className={cn("text-sm shrink-0", variantIconColor[entry.variant])}>
        {variantIcon[entry.variant]}
      </span>
      <span className="text-sm text-fg">{entry.message}</span>
      <button
        type="button"
        aria-label="Закрыть уведомление"
        onClick={onClose}
        className={cn(
          "pointer-events-auto ml-2 shrink-0 inline-flex h-6 w-6 items-center justify-center rounded-xs",
          "text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent",
        )}
      >
        <span aria-hidden="true" className="text-base leading-none">×</span>
      </button>
    </div>
  );
}

/**
 * ToastProvider wires a single app-wide toast channel shared by every
 * provider, hook, and container. It is mounted OUTSIDE QueryClientProvider
 * in main.tsx so the global 401 interceptor (P2-FE-02) and AuthProvider's
 * session-expired handler can surface toast messages from React-Query
 * lifecycle boundaries without creating a context ordering bug.
 *
 * Design notes:
 * - Position: fixed bottom-center (bottom-20 on mobile to clear the bottom
 *   nav, bottom-6 on desktop). Matches UI-kit Toast primitive anchor.
 * - Max visible: 3. Beyond that, oldest toasts are dropped (FIFO) so a
 *   burst of errors doesn't paint the whole screen.
 * - Stacking: flex-col-reverse, newest at the bottom of the viewport so
 *   the most recent message is closest to the user's eye.
 * - Auto-dismiss: 5s default, 7s for errors. Overridable via opts.duration.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  // Monotonic id generator — Date.now() collides inside the same tick when
  // multiple toasts are queued back-to-back (e.g. Promise.all of failures).
  const nextIdRef = useRef(1);

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const push = useCallback(
    (variant: ToastVariant, message: string, duration: number) => {
      const id = nextIdRef.current++;
      setToasts((prev) => {
        const next = [...prev, { id, message, variant, duration }];
        if (next.length > MAX_VISIBLE) {
          return next.slice(next.length - MAX_VISIBLE);
        }
        return next;
      });
      return id;
    },
    [],
  );

  const api = useMemo<ToastAPI>(
    () => ({
      success: (message, opts) =>
        push("success", message, opts?.duration ?? DEFAULT_DURATION),
      error: (message, opts) =>
        push("error", message, opts?.duration ?? ERROR_DURATION),
      info: (message, opts) =>
        push("info", message, opts?.duration ?? DEFAULT_DURATION),
      dismiss,
    }),
    [push, dismiss],
  );

  // Keyboard dismissal: Escape dismisses the most recent (bottom-most) toast
  // whenever any toast is visible. The listener is only attached while the
  // stack is non-empty so we don't swallow Escape for dialogs/menus the rest
  // of the time.
  useEffect(() => {
    if (toasts.length === 0) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setToasts((prev) => (prev.length === 0 ? prev : prev.slice(0, -1)));
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [toasts.length]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      {/*
        Single fixed viewport container hosts all active toasts. Using
        flex-col-reverse so the newest toast sits at the bottom slot — the
        previous toast naturally shifts upward when a new one arrives. Each
        child has pointer-events-auto so the host container stays click-
        through-able (pointer-events-none on the wrapper).
      */}
      {toasts.length > 0 && (
        <div
          className="pointer-events-none fixed bottom-20 md:bottom-6 left-1/2 -translate-x-1/2 z-50 flex flex-col-reverse gap-2 items-center"
        >
          {toasts.map((t) => (
            <StackedToast key={t.id} entry={t} onClose={() => dismiss(t.id)} />
          ))}
        </div>
      )}
    </ToastContext.Provider>
  );
}

/**
 * useToast returns the app-wide ToastAPI. Throws when called outside a
 * ToastProvider — the provider is mounted at the root of main.tsx so a
 * missing context indicates a wiring bug, not a user condition.
 */
export function useToast(): ToastAPI {
  const api = useContext(ToastContext);
  if (!api) {
    throw new Error("useToast() called outside <ToastProvider>");
  }
  return api;
}
