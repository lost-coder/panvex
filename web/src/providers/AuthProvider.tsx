import * as React from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { apiClient, SESSION_EXPIRED_EVENT, type MeResponse } from "@/lib/api";
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
