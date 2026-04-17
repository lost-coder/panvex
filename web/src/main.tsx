import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import React from "react";
import ReactDOM from "react-dom/client";
import { ErrorBoundary } from "@lost-coder/panvex-ui";

import { AppErrorFallback } from "./providers/AppErrorFallback";
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
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <EventsSynchronizer />
          <RouterProvider router={router} context={{ queryClient }} />
        </ToastProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  </React.StrictMode>,
);
