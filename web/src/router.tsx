import { QueryClient, useQueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
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
import { LayoutDashboard, Server, Users, Settings } from "lucide-react";

import { AppShell, type NavItem } from "@panvex/ui";
import { AppearanceProvider } from "@/providers/AppearanceProvider";
import { apiClient } from "@/lib/api";

interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Outlet });

const NAV_ITEMS: NavItem[] = [
  { id: "/", label: "Dashboard", icon: <LayoutDashboard size={20} /> },
  { id: "/servers", label: "Servers", icon: <Server size={20} /> },
  { id: "/clients", label: "Clients", icon: <Users size={20} /> },
  { id: "/settings", label: "Settings", icon: <Settings size={20} /> },
];

function ProtectedShell() {
  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me(),
  });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { location } = useRouterState();

  const activeId =
    NAV_ITEMS.find(
      (item) => item.id !== "/" && location.pathname.startsWith(item.id),
    )?.id ?? "/";

  const handleLogout = async () => {
    try {
      await apiClient.logout();
    } finally {
      queryClient.clear();
      navigate({ to: "/login" });
    }
  };

  return (
    <AppearanceProvider userID={me?.id ?? ""}>
      <AppShell
        navItems={NAV_ITEMS}
        activeId={activeId}
        brand="Panvex"
        onNavigate={(id) => navigate({ to: id })}
        onLogout={handleLogout}
      >
        <Outlet />
      </AppShell>
    </AppearanceProvider>
  );
}

const LoginContainer = lazyRouteComponent(
  () => import("@/containers/LoginContainer").then((m) => ({ default: m.LoginContainer })),
  "default",
);

const DashboardContainer = lazyRouteComponent(
  () =>
    import("@/containers/DashboardContainer").then((m) => ({
      default: m.DashboardContainer,
    })),
  "default",
);

const ServersContainer = lazyRouteComponent(
  () => import("@/containers/ServersContainer").then((m) => ({ default: m.ServersContainer })),
  "default",
);

const ServerDetailContainer = lazyRouteComponent(
  () => import("@/containers/ServerDetailContainer").then((m) => ({ default: m.ServerDetailContainer })),
  "default",
);

const ClientsContainer = lazyRouteComponent(
  () => import("@/containers/ClientsContainer").then((m) => ({ default: m.ClientsContainer })),
  "default",
);

const ClientDetailContainer = lazyRouteComponent(
  () => import("@/containers/ClientDetailContainer").then((m) => ({ default: m.ClientDetailContainer })),
  "default",
);

const DiscoveredClientsContainer = lazyRouteComponent(
  () => import("@/containers/DiscoveredClientsContainer").then((m) => ({ default: m.DiscoveredClientsContainer })),
  "default",
);

const SettingsContainer = lazyRouteComponent(
  () => import("@/containers/SettingsContainer").then((m) => ({ default: m.SettingsContainer })),
  "default",
);

const ProfileContainer = lazyRouteComponent(
  () => import("@/containers/ProfileContainer").then((m) => ({ default: m.ProfileContainer })),
  "default",
);

const UsersContainer = lazyRouteComponent(
  () => import("@/containers/UsersContainer").then((m) => ({ default: m.UsersContainer })),
  "default",
);

const ActivityContainer = lazyRouteComponent(
  () => import("@/containers/ActivityContainer").then((m) => ({ default: m.ActivityContainer })),
  "default",
);

const EnrollmentTokensContainer = lazyRouteComponent(
  () => import("@/containers/EnrollmentTokensContainer").then((m) => ({ default: m.EnrollmentTokensContainer })),
  "default",
);

const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: ProtectedShell,
  beforeLoad: async () => {
    try { await apiClient.me(); }
    catch { throw redirect({ to: "/login" }); }
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

const activityRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/activity",
  component: ActivityContainer,
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
    activityRoute,
    profileRoute,
  ]),
]);

function NotFound() {
  const navigate = useNavigate();
  return (
    <div className="flex flex-col items-center justify-center h-screen gap-4 text-fg-muted">
      <span className="text-6xl font-bold text-fg/20">404</span>
      <p className="text-sm">Page not found</p>
      <button
        onClick={() => navigate({ to: "/" })}
        className="px-4 py-2 text-sm bg-accent text-white rounded-xs hover:bg-accent/90 transition-colors"
      >
        Go to Dashboard
      </button>
    </div>
  );
}

export const router = createRouter({
  routeTree,
  context: { queryClient: undefined! },
  basepath: (window as any).__BASE_PATH__ ?? "/",
  defaultNotFoundComponent: NotFound,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
