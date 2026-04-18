import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import React from "react";
import ReactDOM from "react-dom/client";
import { ErrorBoundary } from "@lost-coder/panvex-ui";

import { AppErrorFallback } from "./providers/AppErrorFallback";
import { ConfirmProvider } from "./providers/ConfirmProvider";
import { EventsSynchronizer } from "./providers/EventsSynchronizer";
import { ToastProvider } from "./providers/ToastProvider";
import { router } from "./router";
import "./styles.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

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
            P2-UX-10: EventsSynchronizer now exposes a WsContext. We wrap
            the router so containers can call `useWsStatus()` to drive the
            reconnection banner and update-flash effects.
            P2-UX-04: ConfirmProvider exposes `useConfirm()` for destructive
            actions. It mounts inside the WS context so confirm dialogs in
            banner-adjacent UI still work when the socket is reconnecting.
          */}
          <EventsSynchronizer>
            <ConfirmProvider>
              <RouterProvider router={router} context={{ queryClient }} />
            </ConfirmProvider>
          </EventsSynchronizer>
        </QueryClientProvider>
      </ToastProvider>
    </ErrorBoundary>
  </React.StrictMode>,
);
