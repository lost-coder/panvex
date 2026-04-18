import { type ViewMode, Spinner } from "@lost-coder/panvex-ui";
import { ClientsPage } from "@/pages/ClientsPage";
import { useClientsList } from "@/hooks/useClientsList";
import { useDiscoveredClients } from "@/hooks/useDiscoveredClients";
import { useClientCreate } from "@/hooks/useClientCreate";
import { useViewMode } from "@/hooks/useViewMode";
import { useNavigate } from "@tanstack/react-router";
import { useUrlSearchState } from "@/hooks/useUrlSearchState";
import { useWsUpdateFlash } from "@/hooks/useWsUpdateFlash";

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
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  const urlView = viewParam === "cards" || viewParam === "list" ? (viewParam as ViewMode) : undefined;
  const effectiveMode = urlView ?? resolveMode(clients.length);

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
