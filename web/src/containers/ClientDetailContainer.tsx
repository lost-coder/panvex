import { ClientDetailPage, Spinner } from "@panvex/ui";
import { useClientDetail } from "@/hooks/useClientDetail";
import { useNavigate, useParams } from "@tanstack/react-router";

export function ClientDetailContainer() {
  const { clientId } = useParams({ strict: false });
  const { client, isLoading } = useClientDetail(clientId ?? "");
  const navigate = useNavigate();

  if (isLoading || !client) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ClientDetailPage
      client={client}
      onBack={() => navigate({ to: "/clients" })}
    />
  );
}
