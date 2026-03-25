import { QueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
} from "@tanstack/react-router";

import { TooltipProvider } from "@/components/ui/tooltip";
import { AppShell } from "@/components/app-shell";
import { AppearanceProvider } from "@/components/appearance-provider";
import { apiClient } from "@/lib/api";

interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Outlet });

function ProtectedShell() {
  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me(),
  });
  return (
    <AppearanceProvider userID={me?.id ?? ""}>
      <TooltipProvider>
        <AppShell><Outlet /></AppShell>
      </TooltipProvider>
    </AppearanceProvider>
  );
}

const LoginPage = lazyRouteComponent(
  () => import("@/features/login/login-page"),
  "LoginPage",
);

const DashboardPage = lazyRouteComponent(
  () => import("@/features/dashboard/dashboard-page"),
  "DashboardPage",
);

const ServersPage = lazyRouteComponent(
  () => import("@/features/servers/servers-page"),
  "ServersPage",
);

const ServerDetailPage = lazyRouteComponent(
  () => import("@/features/servers/server-detail-page"),
  "ServerDetailPage",
);

const ClientsPage = lazyRouteComponent(
  () => import("@/features/clients/clients-page"),
  "ClientsPage",
);

const ClientDetailPage = lazyRouteComponent(
  () => import("@/features/clients/client-detail-page"),
  "ClientDetailPage",
);

const SettingsPage = lazyRouteComponent(
  () => import("@/features/settings/settings-page"),
  "SettingsPage",
);

const ProfilePage = lazyRouteComponent(
  () => import("@/features/profile/profile-page"),
  "ProfilePage",
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
  component: LoginPage,
});

const dashboardRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: DashboardPage,
});

const serversRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/servers",
  component: ServersPage,
});

const serverDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/servers/$serverId",
  component: ServerDetailPage,
});

const clientsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients",
  component: ClientsPage,
});

const clientDetailRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients/$clientId",
  component: ClientDetailPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: SettingsPage,
});

const profileRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/profile",
  component: ProfilePage,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  shellRoute.addChildren([
    dashboardRoute,
    serversRoute,
    serverDetailRoute,
    clientsRoute,
    clientDetailRoute,
    settingsRoute,
    profileRoute,
  ]),
]);

export const router = createRouter({
  routeTree,
  context: { queryClient: undefined! },
  basepath: (window as any).__BASE_PATH__ ?? "/",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
