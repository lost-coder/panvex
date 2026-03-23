import { QueryClient } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from "@tanstack/react-router";

import { TooltipProvider } from "@/components/ui/tooltip";
import { AppShell } from "@/components/app-shell";
import { AppearanceProvider } from "@/components/appearance-provider";
import { apiClient } from "@/lib/api";

import { DashboardPage } from "@/features/dashboard/dashboard-page";
import { ServersPage } from "@/features/servers/servers-page";
import { ClientsPage } from "@/features/clients/clients-page";
import { SettingsPage } from "@/features/settings/settings-page";
import { ProfilePage } from "@/features/profile/profile-page";
import { LoginPage } from "@/features/login/login-page";

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

const clientsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/clients",
  component: ClientsPage,
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
    clientsRoute,
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
