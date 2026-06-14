import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import React from "react";
import ReactDOM from "react-dom/client";
import { z } from "zod";
import { ErrorBoundary } from "@/ui";

import { AppErrorFallback } from "./providers/AppErrorFallback";
import { ToastProvider } from "./providers/ToastProvider";
import { router } from "./router";
import { initI18n } from "@/shared/lib/i18n";
import "../styles.css";

// Disable Zod v4's JIT validator-compilation path. The fast path
// builds per-schema validators dynamically at parse time, which the
// panel's CSP forbids (no `script-src 'unsafe-eval'`). Zod's own
// allowsEval probe trips a CSP violation in the browser console even
// though it catches the failure and falls back gracefully. Setting
// `jitless: true` short-circuits the probe entirely so we get a clean
// console + the safer slow path. Must run before any schema is
// evaluated.
z.config({ jitless: true });

// Phase-3 §3.2: bootstrap i18next before React mounts so the very
// first render of every component already has translations available.
// The active language's resource bundle is fetched as a dynamic chunk
// (i18n-resources-{ru,en}) so we have to await it; subsequent calls
// resolve to the same i18next instance.
const i18nReady = initI18n();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      // staleTime: while a query is "fresh", a re-mount (route change,
      // suspense rehydration, dialog reopen) reuses the cached value
      // instead of refiring the request. WS invalidations bypass this
      // by calling invalidateQueries directly, so live updates are
      // unaffected; the win is fewer redundant fetches during normal
      // navigation.
      staleTime: 10_000,
      // gcTime: drop unsubscribed queries from cache after 5 min so a
      // long-running session does not balloon. Matches React Query's
      // historical default but pinning it makes the lifetime explicit.
      gcTime: 300_000,
    },
  },
});

void i18nReady.then(() => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
    {/*
      Root ErrorBoundary (P1-FE-02 / H10): before this was in place, any
      render error in RouterProvider, a provider, or a container would
      unmount the whole tree with no feedback — effectively a white screen.
      The UI-kit ErrorBoundary renders AppErrorFallback when any child
      throws, giving the operator a Reload button and the error name/
      message for support escalation. ErrorBoundary is the outermost wrap
      so QueryClientProvider failures are also caught.
    */}
    <ErrorBoundary fallback={(error: Error) => <AppErrorFallback error={error} />}>
      {/*
        ToastProvider sits OUTSIDE QueryClientProvider (P2-FE-03) so the
        global 401 handler in AuthProvider and any React-Query lifecycle
        (onError, QueryCache.onError) can surface toasts even when a query
        is mid-flight and a higher provider is re-rendering. The provider
        is pure React state — it has no dependency on QueryClient, so
        hoisting it is safe and avoids a future context ordering bug.
      */}
      <ToastProvider>
        <QueryClientProvider client={queryClient}>
          {/*
            P2-UX-10 / P2-UX-04: EventsSynchronizer (WsContext) and
            ConfirmProvider now live inside the router root (see
            router.tsx RootComponent) so the WebSocket can gate on
            AuthProvider's isAuthenticated. They wrap the route Outlet, so
            every page still gets `useWsStatus()` and `useConfirm()`.
          */}
          <RouterProvider router={router} context={{ queryClient }} />
        </QueryClientProvider>
      </ToastProvider>
      </ErrorBoundary>
    </React.StrictMode>,
  );
});
