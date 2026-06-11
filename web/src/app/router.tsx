// R-Q-24: TanStack Router route definitions naturally collect multiple
// inline component bodies (route components, ProtectedShell, etc.) per
// file. Splitting them apart would scatter the routing tree across a
// dozen files and break the single-source-of-truth pattern recommended
// by TanStack Router. Disable react-refresh fast-refresh on this file
// only — the cost is HMR latency on router edits, not production
// behaviour.
/* eslint-disable react-refresh/only-export-components */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { QueryClient} from "@tanstack/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
  useNavigate,
  useRouterState,
} from "@tanstack/react-router";
import { LayoutDashboard, Server, Users, Settings, Activity, User, Layers, ScrollText, Keyboard } from "lucide-react";

import { AppShell, ErrorBoundary, Spinner, type NavItem } from "@/ui";
import { AppearanceProvider } from "@/app/providers/AppearanceProvider";
import { AppErrorFallback } from "@/app/providers/AppErrorFallback";
import { AuthProvider } from "@/app/providers/AuthProvider";
import { ConfirmProvider, useConfirm } from "@/app/providers/ConfirmProvider";
import { EventsSynchronizer } from "@/app/providers/EventsSynchronizer";
import { OfflineBanner } from "@/components/OfflineBanner";
import { ShortcutsOverlay } from "@/components/ShortcutsOverlay";
import { ThemeToggleButton } from "@/components/ThemeToggleButton";
import { WsStatusBanner } from "@/components/WsStatusBanner";
import { apiClient } from "@/shared/api/api";
import { authKeys } from "@/features/auth/queryKeys";
import { useFocusMainOnRouteChange, useKeyboardShortcut } from "@/shared/hooks";
import { resolveConfiguredRootPath, getRouterBasepath } from "@/shared/lib/runtime-path";

interface RouterContext {
  queryClient: QueryClient;
}

// Root component wraps the route tree in AuthProvider so the global
// 401 listener (P2-FE-02 / M-C12) is mounted inside both the router
// (needs useNavigate) and the QueryClientProvider (needs useQueryClient)
// for every page — login included, so a stale /login tab that receives
// a 401 still gets its cache cleared.
//
// EventsSynchronizer sits INSIDE AuthProvider so it can gate the
// /api/events WebSocket on isAuthenticated: opening it before login
// produced a 401, a spurious "disconnected" banner, and a socket that
// never recovered until a manual reload. ConfirmProvider stays nested
// under it (was previously co-located in main.tsx) so confirm dialogs in
// banner-adjacent UI keep working while the socket reconnects.
function RootComponent() {
  return (
    <AuthProvider>
      <EventsSynchronizer>
        <ConfirmProvider>
          <Outlet />
        </ConfirmProvider>
      </EventsSynchronizer>
    </AuthProvider>
  );
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: RootComponent });

// UX-bottom-nav-limit (Material): the mobile BottomNav must stay ≤5 tabs.
// Labels are i18n keys (ui namespace) resolved inside ProtectedShell —
// module scope has no hooks.
interface NavSpec {
  id: string;
  labelKey: string;
  icon: React.ReactNode;
}

const NAV_PRIMARY_SPEC: NavSpec[] = [
  { id: "/", labelKey: "nav.dashboard", icon: <LayoutDashboard size={20} /> },
  { id: "/servers", labelKey: "nav.servers", icon: <Server size={20} /> },
  { id: "/fleet-groups", labelKey: "nav.fleetGroups", icon: <Layers size={20} /> },
  { id: "/clients", labelKey: "nav.clients", icon: <Users size={20} /> },
];

const NAV_SECONDARY_SPEC: NavSpec[] = [
  { id: "/activity", labelKey: "nav.activity", icon: <Activity size={20} /> },
  // Audit E1: /enrollment-attempts existed as an orphan route — operators
  // debugging a failed enrollment could not reach it. Secondary nav slot
  // next to Activity (it is an observability log, not a daily tab).
  { id: "/enrollment-attempts", labelKey: "nav.enrollmentAttempts", icon: <ScrollText size={20} /> },
  { id: "/settings", labelKey: "nav.settings", icon: <Settings size={20} /> },
  { id: "/profile", labelKey: "nav.profile", icon: <User size={20} /> },
];

function ProtectedShell() {
  const { data: me } = useQuery({
    queryKey: authKeys.me(),
    queryFn: () => apiClient.me(),
  });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { location } = useRouterState();
  const confirm = useConfirm();
  const { t } = useTranslation("ui");
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const navPrimary: NavItem[] = NAV_PRIMARY_SPEC.map((i) => ({ id: i.id, icon: i.icon, label: t(i.labelKey) }));
  const navSecondary: NavItem[] = NAV_SECONDARY_SPEC.map((i) => ({ id: i.id, icon: i.icon, label: t(i.labelKey) }));
  const navItems: NavItem[] = [...navPrimary, ...navSecondary];

  // U-17: the mobile bottom-nav slots must reflect daily-use frequency, not
  // the sidebar's grouping. Clients (the main working surface) and Activity
  // (the "what just happened?" log) earn the four primary slots; Fleet
  // groups — a rarely-touched reference list — moves into the More sheet.
  const BOTTOM_NAV_PRIMARY_IDS = ["/", "/servers", "/clients", "/activity"];
  const bottomNavPrimary: NavItem[] = BOTTOM_NAV_PRIMARY_IDS
    .map((id) => navItems.find((n) => n.id === id))
    .filter((n): n is NavItem => Boolean(n));
  const bottomNavMore: NavItem[] = navItems.filter(
    (n) => !BOTTOM_NAV_PRIMARY_IDS.includes(n.id),
  );

  // W6: move focus to the main landmark on every pathname change so
  // screen-reader and keyboard users land inside the new page instead
  // of staying on the sidebar link they just activated.
  useFocusMainOnRouteChange(location.pathname);

  // UX-13: vim-style navigation. Leader `g` + route letter jumps to the
  // matching page. Shortcuts are skipped while focus is inside an input,
  // so typing "g" into the search box does not teleport the operator.
  // Keep in sync with src/app/shortcuts.ts (overlay + test derive from it).
  useKeyboardShortcut("g d", () => navigate({ to: "/" }));
  useKeyboardShortcut("g s", () => navigate({ to: "/servers" }));
  useKeyboardShortcut("g f", () => navigate({ to: "/fleet-groups" }));
  useKeyboardShortcut("g c", () => navigate({ to: "/clients" }));
  useKeyboardShortcut("g t", () => navigate({ to: "/settings" }));

  const activeId =
    navItems.find(
      (item) => item.id !== "/" && location.pathname.startsWith(item.id),
    )?.id ?? "/";

  const handleLogout = async () => {
    // UX-confirmation-dialogs: logout clears the React Query cache and
    // boots the operator to /login. The sidebar trigger is a 44px target
    // adjacent to the theme toggle, so a mis-tap is plausible — gate it
    // behind a confirm dialog (no type-to-confirm; the action is reversible
    // by signing back in).
    const ok = await confirm({
      title: t("logout.title"),
      body: t("logout.body"),
      confirmLabel: t("logout.confirm"),
      variant: "danger",
    });
    if (!ok) return;
    try {
      await apiClient.logout();
    } finally {
      queryClient.clear();
      void navigate({ to: "/login" });
    }
  };

  return (
    <AppearanceProvider userID={me?.id ?? ""}>
      <AppShell
        navItems={navItems}
        bottomNavItems={bottomNavPrimary}
        bottomNavMoreItems={bottomNavMore}
        activeId={activeId}
        brand="Panvex"
        sidebarFooter={(expanded) => (
          <div className="flex flex-col gap-1">
            <ThemeToggleButton expanded={expanded} />
            <button
              type="button"
              onClick={() => setShortcutsOpen(true)}
              aria-label={t("shortcuts.hint")}
              title={t("shortcuts.hint")}
              className="flex items-center gap-2 w-full rounded-xs px-2 py-2 text-xs text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors"
            >
              <Keyboard size={18} className="shrink-0" aria-hidden="true" />
              {expanded && <span>{t("shortcuts.hintLabel")}</span>}
              {expanded && (
                <kbd className="ml-auto rounded border border-border bg-bg px-1.5 font-mono text-nano">?</kbd>
              )}
            </button>
          </div>
        )}
        onNavigate={(id) => navigate({ to: id })}
        onLogout={handleLogout}
      >
        {/* W15: OS-level network failure sits above WsStatusBanner so
            "offline" and "backend unreachable" remain visually distinct. */}
        <OfflineBanner />
        {/* P2-UX-10: surface reconnection state above all page content. */}
        <WsStatusBanner />
        {/* Route-level error boundary: a crash in one page renders an inline
            fallback while the shell (nav, banners) stays usable, instead of
            the root boundary blowing the whole app to a full-screen reload.
            Keyed by activeId so navigating away auto-resets the error. */}
        <ErrorBoundary key={activeId}>
          <Outlet />
        </ErrorBoundary>
        {/* UX-13: keyboard-shortcut help dialog, toggled by `?`. */}
        <ShortcutsOverlay open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
      </AppShell>
    </AppearanceProvider>
  );
}

const LoginContainer = lazyRouteComponent(
  () => import("@/features/auth/LoginContainer").then((m) => ({ default: m.LoginContainer })),
  "default",
);

const DashboardContainer = lazyRouteComponent(
  () =>
    import("@/features/dashboard/DashboardContainer").then((m) => ({
      default: m.DashboardContainer,
    })),
  "default",
);

const ServersContainer = lazyRouteComponent(
  () => import("@/features/servers/ServersContainer").then((m) => ({ default: m.ServersContainer })),
  "default",
);

const ServerDetailContainer = lazyRouteComponent(
  () => import("@/features/servers/ServerDetailContainer").then((m) => ({ default: m.ServerDetailContainer })),
  "default",
);

const ClientsContainer = lazyRouteComponent(
  () => import("@/features/clients/ClientsContainer").then((m) => ({ default: m.ClientsContainer })),
  "default",
);

const ClientDetailContainer = lazyRouteComponent(
  () => import("@/features/clients/ClientDetailContainer").then((m) => ({ default: m.ClientDetailContainer })),
  "default",
);

const DiscoveredClientsContainer = lazyRouteComponent(
  () => import("@/features/clients/DiscoveredClientsContainer").then((m) => ({ default: m.DiscoveredClientsContainer })),
  "default",
);

const FleetGroupsContainer = lazyRouteComponent(
  () => import("@/features/fleet-groups/FleetGroupsContainer").then((m) => ({ default: m.FleetGroupsContainer })),
  "default",
);

const FleetGroupDetailContainer = lazyRouteComponent(
  () => import("@/features/fleet-groups/FleetGroupDetailContainer").then((m) => ({ default: m.FleetGroupDetailContainer })),
  "default",
);

const SettingsContainer = lazyRouteComponent(
  () => import("@/features/settings/SettingsContainer").then((m) => ({ default: m.SettingsContainer })),
  "default",
);

const ProfileContainer = lazyRouteComponent(
  () => import("@/features/auth/ProfileContainer").then((m) => ({ default: m.ProfileContainer })),
  "default",
);

const UsersContainer = lazyRouteComponent(
  () => import("@/features/users/UsersContainer").then((m) => ({ default: m.UsersContainer })),
  "default",
);

const ActivityContainer = lazyRouteComponent(
  () => import("@/features/activity/ActivityContainer").then((m) => ({ default: m.ActivityContainer })),
  "default",
);

const EnrollmentTokensContainer = lazyRouteComponent(
  () => import("@/features/enrollment/EnrollmentTokensContainer").then((m) => ({ default: m.EnrollmentTokensContainer })),
  "default",
);

const AddServerContainer = lazyRouteComponent(
  () => import("@/features/servers/AddServerContainer").then((m) => ({ default: m.AddServerContainer })),
  "default",
);

// Phase-3 §3.b enrollment-attempts page. Fleet-wide observability view
// for the GET /api/enrollment-attempts list; deep-linkable from the
// per-agent EnrollmentHistory fold via `?agent_id=…`.
const EnrollmentAttemptsPage = lazyRouteComponent(
  () =>
    import("@/features/enrollment-attempts/EnrollmentAttemptsPage").then((m) => ({
      default: m.EnrollmentAttemptsPage,
    })),
  "default",
);

const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: ProtectedShell,
  // P2-FE-05 / M-P5: route into the QueryClient cache instead of firing a
  // fresh `apiClient.me()` on every navigation. ensureQueryData reuses the
  // same authKeys.me() entry that ProtectedShell/AuthProvider already read,
  // so a navigation inside the 30s staleTime is a cache hit — no extra
  // round trip. A 401 still rejects, and the catch branch redirects to
  // /login.
  beforeLoad: async ({ context }) => {
    try {
      await context.queryClient.ensureQueryData({
        queryKey: authKeys.me(),
        queryFn: () => apiClient.me(),
        staleTime: 30_000,
      });
    } catch {
      throw redirect({ to: "/login" });
    }
  },
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginContainer,
});

const dashboardRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: DashboardContainer,
});

const serversRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/servers",
  component: ServersContainer,
});

const serverDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/servers/$serverId",
  component: ServerDetailContainer,
});

const clientsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients",
  component: ClientsContainer,
});

const clientDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients/$clientId",
  component: ClientDetailContainer,
});

const discoveredClientsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients/discovered",
  component: DiscoveredClientsContainer,
});

const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: SettingsContainer,
});

const profileRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/profile",
  component: ProfileContainer,
});

const usersRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings/users",
  component: UsersContainer,
});

const enrollmentTokensRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/servers/enrollment",
  component: EnrollmentTokensContainer,
});

const addServerRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/servers/add",
  component: AddServerContainer,
});

const enrollmentAttemptsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/enrollment-attempts",
  component: EnrollmentAttemptsPage,
});

const activityRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/activity",
  component: ActivityContainer,
});

const fleetGroupsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/fleet-groups",
  component: FleetGroupsContainer,
});

const fleetGroupDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/fleet-groups/$fleetGroupId",
  component: FleetGroupDetailContainer,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  shellRoute.addChildren([
    dashboardRoute,
    serversRoute,
    serverDetailRoute,
    clientsRoute,
    discoveredClientsRoute,
    clientDetailRoute,
    settingsRoute,
    usersRoute,
    enrollmentTokensRoute,
    addServerRoute,
    enrollmentAttemptsRoute,
    activityRoute,
    fleetGroupsRoute,
    fleetGroupDetailRoute,
    profileRoute,
  ]),
]);

function NotFound() {
  const navigate = useNavigate();
  const { t } = useTranslation("ui");
  return (
    <div className="flex flex-col items-center justify-center h-screen gap-4 text-fg-muted">
      <span className="text-6xl font-bold text-fg/20">404</span>
      <p className="text-sm">{t("notFound.title")}</p>
      <button
        onClick={() => navigate({ to: "/" })}
        className="px-4 py-2 text-sm bg-accent text-white rounded-xs hover:bg-accent/90 transition-colors"
      >
        {t("notFound.goDashboard")}
      </button>
    </div>
  );
}

// Default error/pending components apply to every route in the tree
// unless that route opts out by declaring its own. This way a lazy
// chunk that fails to load shows AppErrorFallback (full-page boundary
// with a Reload button + the error message) instead of a white screen,
// and any in-flight loader renders Spinner instead of leaving the
// previous page's content stale.
//
// defaultPendingMs delays the spinner so fast loads (cache hit, local
// API ~30ms) skip the fallback entirely; only loads slower than 200ms
// render the spinner. defaultPendingMinMs prevents a flash if the
// load resolves a few ms after we crossed the threshold.
function RoutePending() {
  return (
    <div className="flex items-center justify-center py-16">
      <Spinner size="lg" />
    </div>
  );
}

function RouteErrorBoundary({ error }: Readonly<{ error: Error }>) {
  return <AppErrorFallback error={error} />;
}

export const router = createRouter({
  routeTree,
  context: { queryClient: undefined! },
  basepath: getRouterBasepath(resolveConfiguredRootPath()),
  defaultNotFoundComponent: NotFound,
  defaultErrorComponent: RouteErrorBoundary,
  defaultPendingComponent: RoutePending,
  defaultPendingMs: 200,
  defaultPendingMinMs: 400,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
