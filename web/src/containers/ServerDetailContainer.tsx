import { ServerDetailPage, Spinner } from "@panvex/ui";
import { useServerDetail } from "@/hooks/useServerDetail";
import { useServerMutations } from "@/hooks/useServerMutations";
import { useNavigate, useParams } from "@tanstack/react-router";
import { transformAgentConnection } from "@/lib/transforms/servers";

export function ServerDetailContainer() {
  const { serverId } = useParams({ strict: false });
  const { server, initState, lastUpdatedAt, raw, isLoading } = useServerDetail(serverId ?? "");
  const {
    allowCertRecoveryMutation,
    revokeCertRecoveryMutation,
    boostDetailMutation,
    renameMutation,
    deregisterMutation,
  } = useServerMutations(serverId ?? "");
  const navigate = useNavigate();

  if (isLoading || !server) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ServerDetailPage
      server={server}
      initState={initState}
      lastUpdatedAt={lastUpdatedAt}
      onBack={() => navigate({ to: "/servers" })}
      onBoostDetail={() => boostDetailMutation.mutate()}
      agentConnection={transformAgentConnection(raw?.server?.agent)}
      onAllowReEnrollment={() => allowCertRecoveryMutation.mutate()}
      onRevokeGrant={() => revokeCertRecoveryMutation.mutate()}
      onRename={(name: string) => renameMutation.mutate(name)}
      onDeregister={() => {
        deregisterMutation.mutate(undefined, {
          onSuccess: () => navigate({ to: "/servers" }),
        });
      }}
    />
  );
}
