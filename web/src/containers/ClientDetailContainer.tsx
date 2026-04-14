import { useState, useEffect } from "react";
import { ClientDetailPage, Spinner } from "@lost-coder/panvex-ui";
import { useClientDetail } from "@/hooks/useClientDetail";
import { useClientMutations } from "@/hooks/useClientMutations";
import { useClientIPHistory } from "@/hooks/useClientIPHistory";
import { useNavigate, useParams } from "@tanstack/react-router";

export function ClientDetailContainer() {
  const { clientId } = useParams({ strict: false });
  const { client, raw, isLoading } = useClientDetail(clientId ?? "");
  const { editMutation, rotateMutation, deleteMutation } = useClientMutations(clientId ?? "", raw);
  const { ips, totalUnique } = useClientIPHistory(clientId ?? "");
  const navigate = useNavigate();
  const [secretPending, setSecretPending] = useState(false);

  // Reset pending state when fresh server data arrives after rotation
  useEffect(() => {
    if (raw && secretPending) {
      setSecretPending(false);
    }
  }, [raw]);

  if (isLoading || !client) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ClientDetailPage
      client={client}
      onBack={() => navigate({ to: "/clients" })}
      onEdit={async (data) => {
        await editMutation.mutateAsync(data);
      }}
      editLoading={editMutation.isPending}
      editError={editMutation.error?.message}
      onRotateSecret={async () => {
        await rotateMutation.mutateAsync();
        setSecretPending(true);
      }}
      secretRotating={rotateMutation.isPending}
      secretPendingRedeploy={secretPending}
      onDelete={async () => {
        await deleteMutation.mutateAsync();
        navigate({ to: "/clients" });
      }}
      ipHistory={ips.length > 0 ? { ips, totalUnique } : undefined}
    />
  );
}
