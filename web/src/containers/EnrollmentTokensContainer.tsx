import { EnrollmentTokensPage, Spinner } from "@panvex/ui";
import { useEnrollmentTokens } from "@/hooks/useEnrollmentTokens";
import { ErrorState } from "@/components/ErrorState";

export function EnrollmentTokensContainer() {
  const { tokens, isLoading, error, createToken, revokeToken } = useEnrollmentTokens();

  const handleCreate = () => {
    createToken.mutate({ fleet_group_id: "", ttl_seconds: 86400 });
  };

  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
  }

  return (
    <EnrollmentTokensPage
      tokens={tokens}
      onCreateToken={handleCreate}
      onRevoke={(value) => revokeToken.mutate(value)}
    />
  );
}
