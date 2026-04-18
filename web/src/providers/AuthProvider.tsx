import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  apiClient,
  FORBIDDEN_EVENT,
  SESSION_EXPIRED_EVENT,
  type ForbiddenEventDetail,
  type MeResponse,
} from "@/lib/api";
import { useToast } from "@/providers/ToastProvider";

interface AuthContextValue {
  user: MeResponse | null;
  isLoading: boolean;
  isAuthenticated: boolean;
}

const AuthContext = React.createContext<AuthContextValue>({
  user: null,
  isLoading: true,
  isAuthenticated: false,
});

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const { data, isLoading } = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me(),
    retry: false,
  });

  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const toast = useToast();

  // Global 401 listener (P2-FE-02 / M-C12 / DF-12): api.ts dispatches
  // SESSION_EXPIRED_EVENT when any authenticated request returns 401.
  // We own router + cache access here, so we clear all cached queries
  // (to prevent a stale dashboard from flashing after re-login) and
  // navigate to /login. Guarded so we don't re-navigate if already on
  // /login (e.g. the login form itself produced a 401 — though api.ts
  // also skips dispatch for /auth/login and /auth/me to avoid loops).
  //
  // P2-FE-03: surface an info toast before the redirect so the operator
  // understands why the page just changed — otherwise a silent redirect
  // from a data page to /login looks like the app crashed.
  React.useEffect(() => {
    const handler = () => {
      queryClient.clear();
      if (typeof window !== "undefined" &&
          window.location.pathname.endsWith("/login")) {
        return;
      }
      toast.info("Сессия истекла, переход на /login…");
      navigate({ to: "/login" });
    };
    window.addEventListener(SESSION_EXPIRED_EVENT, handler);
    return () => {
      window.removeEventListener(SESSION_EXPIRED_EVENT, handler);
    };
  }, [queryClient, navigate, toast]);

  // Global 403 listener (W13): api.ts dispatches FORBIDDEN_EVENT whenever
  // an authenticated request returns 403 outside the auth bootstrap. The
  // mutation-level onError may also surface a toast with the server
  // message; this listener exists so even unhandled 403s produce a single,
  // human-friendly cue instead of a silent failure.
  //
  // The listener is debounced at a coarse granularity: we ignore repeat
  // 403s from the same path within 1500 ms, which prevents a burst (e.g.
  // React Query retrying a few times) from stacking three identical
  // "Недостаточно прав…" toasts on top of each other.
  React.useEffect(() => {
    let lastPath = "";
    let lastAt = 0;
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<ForbiddenEventDetail>).detail;
      const now = Date.now();
      if (detail?.path === lastPath && now - lastAt < 1500) {
        return;
      }
      lastPath = detail?.path ?? "";
      lastAt = now;
      toast.error("Недостаточно прав для этой операции. Обратитесь к администратору.");
    };
    window.addEventListener(FORBIDDEN_EVENT, handler);
    return () => {
      window.removeEventListener(FORBIDDEN_EVENT, handler);
    };
  }, [toast]);

  const value: AuthContextValue = {
    user: data ?? null,
    isLoading,
    isAuthenticated: !!data,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  return React.useContext(AuthContext);
}
