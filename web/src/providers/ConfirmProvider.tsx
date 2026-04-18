// P2-UX-04: global confirmation dialog for destructive actions.
//
// Pattern (as specified by the remediation plan):
//
//   const confirm = useConfirm();
//   const ok = await confirm({
//     title: "Delete client?",
//     body: "This removes the client from all nodes.",
//     confirmLabel: "Delete",
//     variant: "danger",
//   });
//   if (ok) { await deleteMutation.mutateAsync(); }
//
// Implementation notes:
// - Single in-flight confirmation. Opening a second call rejects the first
//   (the caller treats "false" as cancelled). This keeps state simple and
//   matches how operators actually click: one destructive action at a time.
// - Uses the UI-kit <ConfirmDialog/> which is built on <dialog>, so focus
//   and Escape are handled natively by the browser without extra wiring.

import { createContext, useCallback, useContext, useMemo, useRef, useState } from "react";
import { ConfirmDialog } from "@lost-coder/panvex-ui";

export interface ConfirmOptions {
  title: string;
  body: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "danger";
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;

const ConfirmContext = createContext<ConfirmFn | null>(null);

interface InternalState {
  open: boolean;
  options: ConfirmOptions;
}

const EMPTY_STATE: InternalState = {
  open: false,
  options: { title: "", body: "" },
};

export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<InternalState>(EMPTY_STATE);
  const resolverRef = useRef<((result: boolean) => void) | null>(null);

  const confirm = useCallback<ConfirmFn>((opts) => {
    // Resolve any in-flight confirmation as cancelled before opening the
    // new one. Prevents orphaned promises when a second confirm() lands
    // during rapid action sequences.
    if (resolverRef.current) {
      resolverRef.current(false);
      resolverRef.current = null;
    }
    return new Promise<boolean>((resolve) => {
      resolverRef.current = resolve;
      setState({ open: true, options: opts });
    });
  }, []);

  const settle = useCallback((result: boolean) => {
    const r = resolverRef.current;
    resolverRef.current = null;
    setState((prev) => ({ ...prev, open: false }));
    if (r) r(result);
  }, []);

  const value = useMemo(() => confirm, [confirm]);

  return (
    <ConfirmContext.Provider value={value}>
      {children}
      <ConfirmDialog
        open={state.open}
        title={state.options.title}
        description={state.options.body}
        confirmLabel={state.options.confirmLabel}
        cancelLabel={state.options.cancelLabel}
        variant={state.options.variant}
        onConfirm={() => settle(true)}
        onCancel={() => settle(false)}
      />
    </ConfirmContext.Provider>
  );
}

export function useConfirm(): ConfirmFn {
  const ctx = useContext(ConfirmContext);
  if (!ctx) {
    throw new Error(
      "useConfirm must be used within a <ConfirmProvider> (see main.tsx).",
    );
  }
  return ctx;
}
