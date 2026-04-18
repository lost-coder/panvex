import { useState, useEffect } from "react";
import { Spinner } from "@lost-coder/panvex-ui";
import { ClientDetailPage } from "@/pages/ClientDetailPage";
import { useClientDetail } from "@/hooks/useClientDetail";
import { useClientMutations } from "@/hooks/useClientMutations";
import { useClientIPHistory } from "@/hooks/useClientIPHistory";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useConfirm } from "@/providers/ConfirmProvider";

export function ClientDetailContainer() {
  const { clientId } = useParams({ strict: false });
  const { client, raw, isLoading } = useClientDetail(clientId ?? "");
  const { editMutation, rotateMutation, deleteMutation } = useClientMutations(clientId ?? "", raw);
  const { ips, totalUnique } = useClientIPHistory(clientId ?? "");
  const navigate = useNavigate();
  const confirm = useConfirm();
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
      // P2-UX-04: rotating a secret invalidates all client devices — gate
      // it behind a confirm dialog so an accidental click doesn't lock users out.
      onRotateSecret={async () => {
        const ok = await confirm({
          title: "Rotate client secret?",
          body: `Existing connection links for "${client.name}" will stop working until operators redeploy them.`,
          confirmLabel: "Rotate secret",
          variant: "danger",
        });
        if (!ok) return;
        await rotateMutation.mutateAsync();
        setSecretPending(true);
      }}
      secretRotating={rotateMutation.isPending}
      secretPendingRedeploy={secretPending}
      // P2-UX-04: destructive and irreversible — the confirm dialog is the
      // last safety net before the client row disappears fleet-wide.
      onDelete={async () => {
        const ok = await confirm({
          title: "Delete client?",
          body: `This removes "${client.name}" from every node and revokes all its connection links.`,
          confirmLabel: "Delete client",
          variant: "danger",
        });
        if (!ok) return;
        await deleteMutation.mutateAsync();
        navigate({ to: "/clients" });
      }}
      ipHistory={ips.length > 0 ? { ips, totalUnique } : undefined}
    />
  );
}
