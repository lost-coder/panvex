import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useEffect, useMemo, useRef, useState } from "react";

import { apiClient, configuredRootPath } from "../lib/api";
import { getSidebarNavigation, getUserMenuItems } from "../profile-and-settings-state";
import { buildEventsURL } from "../lib/runtime-path";

export function AppShell() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [live, setLive] = useState(false);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const userMenuRef = useRef<HTMLDivElement | null>(null);
  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me()
  });
  const navigation = useMemo(() => getSidebarNavigation(meQuery.data?.role), [meQuery.data?.role]);
  const logoutMutation = useMutation({
    mutationFn: () => apiClient.logout(),
    onSuccess: async () => {
      setUserMenuOpen(false);
      queryClient.removeQueries({ queryKey: ["appearance-settings"] });
      await queryClient.invalidateQueries({ queryKey: ["me"] });
      await navigate({ to: "/login" });
    }
  });

  useEffect(() => {
    if (!meQuery.data) {
      return;
    }

    const socket = new WebSocket(buildEventsURL(window.location.protocol, window.location.host, configuredRootPath));
    socket.onopen = () => setLive(true);
    socket.onclose = () => setLive(false);
    socket.onmessage = () => {
      void queryClient.invalidateQueries({ queryKey: ["control-room"] });
      void queryClient.invalidateQueries({ queryKey: ["fleet"] });
      void queryClient.invalidateQueries({ queryKey: ["agents"] });
      void queryClient.invalidateQueries({ queryKey: ["instances"] });
      void queryClient.invalidateQueries({ queryKey: ["jobs"] });
      void queryClient.invalidateQueries({ queryKey: ["audit"] });
      void queryClient.invalidateQueries({ queryKey: ["metrics"] });
      void queryClient.invalidateQueries({ queryKey: ["enrollment-tokens"] });
      void queryClient.invalidateQueries({ queryKey: ["clients"] });
      void queryClient.invalidateQueries({ queryKey: ["client"] });
    };

    return () => socket.close();
  }, [meQuery.data, queryClient]);

  useEffect(() => {
    setUserMenuOpen(false);
  }, [navigation, pathname]);

  useEffect(() => {
    if (!userMenuOpen) {
      return;
    }

    const handlePointerDown = (event: MouseEvent) => {
      if (userMenuRef.current && !userMenuRef.current.contains(event.target as Node)) {
        setUserMenuOpen(false);
      }
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setUserMenuOpen(false);
      }
    };

    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleEscape);

    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleEscape);
    };
  }, [userMenuOpen]);

  const title = useMemo(() => {
    if (pathname.startsWith("/profile")) {
      return "Profile";
    }
    if (pathname.startsWith("/settings")) {
      return "Settings";
    }

    return navigation.find((item) => pathname === item.to || (item.to !== "/" && pathname.startsWith(item.to)))?.label ?? "Dashboard";
  }, [navigation, pathname]);

  return (
    <div className="app-page min-h-screen">
      <div className="mx-auto flex min-h-screen max-w-[1600px] gap-6 px-4 py-4 lg:px-6">
        <aside className="app-shell-panel hidden w-72 shrink-0 rounded-[28px] p-5 lg:flex lg:flex-col">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--app-text-tertiary)]">Panvex</p>
            <h1 className="mt-3 text-2xl font-semibold tracking-tight text-[var(--app-text-primary)]">Control Room</h1>
            <p className="mt-2 text-sm text-[var(--app-text-secondary)]">A calm home for your Telemt servers, day-to-day actions, and live health.</p>
          </div>

          <nav className="mt-8 space-y-2">
            {navigation.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                className="block rounded-2xl px-4 py-3 text-sm font-medium text-[var(--app-text-secondary)] transition hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text-primary)]"
                activeProps={{
                  className: "app-nav-active block rounded-2xl px-4 py-3 text-sm font-medium"
                }}
              >
                {item.label}
              </Link>
            ))}
          </nav>

          <div className="app-sidebar-status mt-auto rounded-3xl p-4">
            <div className="flex items-center justify-between text-xs uppercase tracking-[0.22em] text-[var(--app-sidebar-status-muted)]">
              <span>Realtime</span>
              <span className={`inline-flex h-2.5 w-2.5 rounded-full ${live ? "bg-emerald-400" : "bg-amber-400"}`} />
            </div>
            <p className="mt-3 text-sm text-[var(--app-sidebar-status-text)]">
              {live ? "Live updates are flowing in" : "Waiting for the live event stream"}
            </p>
          </div>
        </aside>

        <div className="flex min-h-screen min-w-0 flex-1 flex-col">
          <header className="app-shell-panel relative z-30 rounded-[28px] px-5 py-4">
            <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
              <div>
                <h2 className="text-3xl font-semibold tracking-tight text-[var(--app-text-primary)]">{title}</h2>
              </div>
              <div className="relative" ref={userMenuRef}>
                {meQuery.data ? (
                  <>
                    <button
                      type="button"
                      className="app-user-button rounded-2xl px-4 py-3 text-left text-sm"
                      onClick={() => setUserMenuOpen((open) => !open)}
                    >
                      <div className="space-y-1">
                        <div className="font-medium text-[var(--app-text-primary)]">{meQuery.data.username}</div>
                        <div className="uppercase tracking-[0.2em] text-[var(--app-text-tertiary)]">{meQuery.data.role}</div>
                      </div>
                    </button>
                    {userMenuOpen ? (
                      <div className="app-user-menu-panel absolute right-0 top-[calc(100%+0.75rem)] z-50 min-w-52 rounded-2xl p-2">
                        {getUserMenuItems().map((item) =>
                          item.kind === "link" ? (
                            <Link
                              key={item.to}
                              to={item.to}
                              className="app-user-menu-item block rounded-xl px-3 py-2.5 text-sm font-medium"
                              onClick={() => setUserMenuOpen(false)}
                            >
                              {item.label}
                            </Link>
                          ) : (
                            <button
                              key={item.action}
                              type="button"
                              className="app-user-menu-item block w-full rounded-xl px-3 py-2.5 text-left text-sm font-medium"
                              onClick={() => logoutMutation.mutate()}
                              disabled={logoutMutation.isPending}
                            >
                              {logoutMutation.isPending ? "Logging out..." : item.label}
                            </button>
                          )
                        )}
                      </div>
                    ) : null}
                  </>
                ) : (
                  "Authenticating..."
                )}
              </div>
            </div>
          </header>

          <main className="mt-6 flex-1">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  );
}
