import { SectionPanel } from "@/components/section-panel";
import { Badge } from "@/components/ui/badge";
import { useClientDetail } from "./clients-state";

export function ClientDetailPage({ clientId }: { clientId: string }) {
  const { data: client, isLoading } = useClientDetail(clientId);

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <div className="animate-pulse bg-surface h-8 w-48 rounded" />
        <div className="animate-pulse bg-surface h-48 rounded" />
      </div>
    );
  }

  if (!client) return <div className="p-6 text-text-3">Client not found.</div>;

  return (
    <div className="p-6 space-y-4">
      <div>
        <h1 className="text-xl font-bold text-text-1">{client.name}</h1>
        <p className="text-sm text-text-3 mt-0.5">Client ID: {client.id}</p>
      </div>
      <SectionPanel title="Client Info">
        <div className="p-4 space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-xs text-text-3">Status</span>
            <Badge variant={client.enabled ? "good" : "bad"}>
              {client.enabled ? "Enabled" : "Disabled"}
            </Badge>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-xs text-text-3">Assigned Servers</span>
            <span className="text-sm font-semibold text-text-1">{client.agent_ids.length}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-xs text-text-3">Active Connections</span>
            <span className="text-sm font-semibold text-text-1">{client.active_tcp_conns}</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-xs text-text-3">Deployments</span>
            <span className="text-sm text-text-2">{client.deployments.length}</span>
          </div>
        </div>
      </SectionPanel>
    </div>
  );
}
