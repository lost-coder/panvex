import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { Toast, type ToastVariant } from "@lost-coder/panvex-ui";

// Public API for app code.
export interface ToastAPI {
  success(message: string, opts?: { duration?: number }): void;
  error(message: string, opts?: { duration?: number }): void;
  info(message: string, opts?: { duration?: number }): void;
}

interface ToastState {
  id: number;
  message: string;
  variant: ToastVariant;
  duration: number;
}

const ToastContext = createContext<ToastAPI | null>(null);

/**
 * ToastProvider wires a single app-wide toast channel. Only one toast is
 * visible at a time — newer toasts replace older ones. Error toasts use a
 * longer duration (7s) so critical failure messages are not missed; success
 * toasts use the UI-kit default (3s).
 *
 * Consumers call useToast() and receive the ToastAPI surface. This is the
 * foundation for P2-FE-01b (Zod validation errors) and P2-FE-02 (global 401
 * interceptor) — both surface user-facing failures here.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastState | null>(null);

  const push = useCallback(
    (variant: ToastVariant, message: string, duration: number) => {
      setToast({ id: Date.now(), message, variant, duration });
    },
    [],
  );

  const api = useMemo<ToastAPI>(
    () => ({
      success: (message, opts) => push("success", message, opts?.duration ?? 3000),
      error: (message, opts) => push("error", message, opts?.duration ?? 7000),
      info: (message, opts) => push("info", message, opts?.duration ?? 3500),
    }),
    [push],
  );

  const handleClose = useCallback(() => setToast(null), []);

  return (
    <ToastContext.Provider value={api}>
      {children}
      {toast ? (
        <Toast
          key={toast.id}
          message={toast.message}
          variant={toast.variant}
          duration={toast.duration}
          open
          onClose={handleClose}
        />
      ) : null}
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
