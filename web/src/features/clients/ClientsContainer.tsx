import { type ViewMode, EmptyState } from "@/ui";
import { ClientsPage } from "@/features/clients/ClientsPage";
import { SkeletonRows } from "@/components/Skeleton";
import { useClientsList } from "./hooks/useClientsList";
import { useDiscoveredClients } from "./hooks/useDiscoveredClients";
import { useClientCreate } from "./hooks/useClientCreate";
import { useViewMode } from "@/shared/hooks/useViewMode";
import { useNavigate } from "@tanstack/react-router";
import { useUrlSearchState } from "@/shared/hooks/useUrlSearchState";
import { useWsUpdateFlash } from "@/shared/hooks/useWsUpdateFlash";

export function ClientsContainer() {
  const { clients, isLoading } = useClientsList();
  const { discoveredClients } = useDiscoveredClients();
  const createMutation = useClientCreate();
  const { resolveMode, setMode } = useViewMode("clients");
  const navigate = useNavigate();
  const flashing = useWsUpdateFlash();

  // P2-UX-05: persist viewMode in the URL. localStorage (useViewMode)
  // still owns the long-lived preference; the URL copy is the sharable
  // layer so a deep-link lands peers in the same card/list state.
  const [viewParam, setViewParam] = useUrlSearchState("view", "");

  const pendingCount = discoveredClients.filter((dc) => dc.status === "pending_review").length;

  if (isLoading) {
    // 2.5: skeleton instead of a full-height spinner so the layout
    // slot matches the eventual list and there is no content flash
    // when the data arrives.
    return (
      <div className="p-4">
        <SkeletonRows count={6} />
      </div>
    );
  }

  const urlView = viewParam === "cards" || viewParam === "list" ? (viewParam as ViewMode) : undefined;
  const effectiveMode = urlView ?? resolveMode(clients.length);

  // 2.5: empty state invites the first-time operator to create a
  // client instead of presenting a blank page that looks broken.
  if (clients.length === 0) {
    return (
      <div className="p-6">
        <EmptyState
          icon="👥"
          title="Клиентов пока нет"
          description="Создайте первого клиента, чтобы начать назначать его на ноды Telemt."
        />
      </div>
    );
  }

  return (
    // P2-UX-10: subtle ring flash when the underlying query revalidates
    // due to a WS event. Transition is short (1.2s) so it doesn't linger.
    <div className={flashing ? "transition-[box-shadow] duration-300 ring-2 ring-accent/20 rounded" : undefined}>
      <ClientsPage
        clients={clients}
        viewMode={effectiveMode}
        autoThreshold={10}
        onViewModeChange={(m) => {
          setMode(m);
          setViewParam(m);
        }}
        onClientClick={(id) => navigate({ to: "/clients/$clientId", params: { clientId: id } })}
        onCreate={async (data) => { await createMutation.mutateAsync(data); }}
        createLoading={createMutation.isPending}
        createError={createMutation.error?.message}
        pendingDiscoveredCount={pendingCount}
        onDiscoveredClick={() => navigate({ to: "/clients/discovered" })}
      />
    </div>
  );
}
