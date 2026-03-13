import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useRouterState } from "@tanstack/react-router";
import { useEffect, useMemo, useState } from "react";

import { apiClient } from "../lib/api";

const navigation = [
  { to: "/", label: "Overview" },
  { to: "/fleet", label: "Fleet" },
  { to: "/jobs", label: "Jobs" },
  { to: "/audit", label: "Audit" },
  { to: "/agents", label: "Agents" },
  { to: "/settings", label: "Settings" }
] as const;

export function AppShell() {
  const queryClient = useQueryClient();
  const [live, setLive] = useState(false);
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const meQuery = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me()
  });

  useEffect(() => {
    if (!meQuery.data) {
      return;
    }

    const socket = new WebSocket(`${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/events`);
    socket.onopen = () => setLive(true);
    socket.onclose = () => setLive(false);
    socket.onmessage = () => {
      void queryClient.invalidateQueries({ queryKey: ["fleet"] });
      void queryClient.invalidateQueries({ queryKey: ["agents"] });
      void queryClient.invalidateQueries({ queryKey: ["instances"] });
      void queryClient.invalidateQueries({ queryKey: ["jobs"] });
      void queryClient.invalidateQueries({ queryKey: ["audit"] });
      void queryClient.invalidateQueries({ queryKey: ["metrics"] });
    };

    return () => socket.close();
  }, [meQuery.data, queryClient]);

  const title = useMemo(() => {
    return navigation.find((item) => item.to === pathname)?.label ?? "Control Room";
  }, [pathname]);

  return (
    <div className="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(28,95,140,0.16),_transparent_28%),linear-gradient(180deg,#f5f2ea_0%,#f6f8fb_100%)] text-slate-900">
      <div className="mx-auto flex min-h-screen max-w-[1600px] gap-6 px-4 py-4 lg:px-6">
        <aside className="hidden w-72 shrink-0 rounded-[28px] border border-white/70 bg-white/80 p-5 shadow-[0_20px_60px_rgba(37,46,68,0.08)] backdrop-blur lg:flex lg:flex-col">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Panvex</p>
            <h1 className="mt-3 text-2xl font-semibold tracking-tight text-slate-950">Control Room</h1>
            <p className="mt-2 text-sm text-slate-600">Fleet orchestration for Telemt nodes.</p>
          </div>

          <nav className="mt-8 space-y-2">
            {navigation.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                className="block rounded-2xl px-4 py-3 text-sm font-medium text-slate-600 transition hover:bg-slate-950/5 hover:text-slate-950"
                activeProps={{
                  className: "block rounded-2xl bg-slate-950 px-4 py-3 text-sm font-medium text-white shadow-[0_16px_32px_rgba(15,23,42,0.18)]"
                }}
              >
                {item.label}
              </Link>
            ))}
          </nav>

          <div className="mt-auto rounded-3xl border border-slate-200/80 bg-slate-950/95 p-4 text-white">
            <div className="flex items-center justify-between text-xs uppercase tracking-[0.22em] text-white/70">
              <span>Realtime</span>
              <span className={`inline-flex h-2.5 w-2.5 rounded-full ${live ? "bg-emerald-400" : "bg-amber-400"}`} />
            </div>
            <p className="mt-3 text-sm text-white/80">
              {live ? "Live event stream connected" : "Waiting for event stream"}
            </p>
          </div>
        </aside>

        <div className="flex min-h-screen min-w-0 flex-1 flex-col">
          <header className="rounded-[28px] border border-white/70 bg-white/80 px-5 py-4 shadow-[0_20px_60px_rgba(37,46,68,0.08)] backdrop-blur">
            <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
              <div>
                <p className="text-xs font-semibold uppercase tracking-[0.24em] text-slate-500">Operations</p>
                <h2 className="mt-2 text-3xl font-semibold tracking-tight text-slate-950">{title}</h2>
              </div>
              <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-600">
                {meQuery.data ? (
                  <div className="space-y-1">
                    <div className="font-medium text-slate-900">{meQuery.data.username}</div>
                    <div className="uppercase tracking-[0.2em] text-slate-500">{meQuery.data.role}</div>
                  </div>
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
