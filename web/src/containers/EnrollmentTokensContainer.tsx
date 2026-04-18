import { EnrollmentTokensPage, Spinner } from "@lost-coder/panvex-ui";
import { useEnrollmentTokens } from "@/hooks/useEnrollmentTokens";
import { ErrorState } from "@/components/ErrorState";
import { useConfirm } from "@/providers/ConfirmProvider";

export function EnrollmentTokensContainer() {
  const { tokens, isLoading, error, createToken, revokeToken } = useEnrollmentTokens();
  const confirm = useConfirm();

  const handleCreate = () => {
    createToken.mutate({ fleet_group_id: "", ttl_seconds: 86400 });
  };

  const handleRevoke = async (value: string) => {
    // P2-UX-04: revoking a token a pending enrollment depends on leaves
    // the agent stuck mid-bootstrap. Confirm before the point of no return.
    const ok = await confirm({
      title: "Revoke this enrollment token?",
      body: "Any agent still mid-bootstrap with this token will fail to enroll. Issue a new token if needed.",
      confirmLabel: "Revoke",
      variant: "danger",
    });
    if (!ok) return;
    revokeToken.mutate(value);
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
      onRevoke={handleRevoke}
    />
  );
}
