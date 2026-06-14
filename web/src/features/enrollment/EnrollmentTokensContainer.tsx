import { EnrollmentTokensPage } from "./EnrollmentTokensPage";
import { useEnrollmentTokens } from "./hooks/useEnrollmentTokens";
import { ErrorState } from "@/components/ErrorState";
import { SkeletonRows } from "@/ui";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { useNavigate } from "@tanstack/react-router";

export function EnrollmentTokensContainer() {
  const { tokens, isLoading, error, refetch, createToken, revokeToken } = useEnrollmentTokens();
  const confirm = useConfirm();
  const navigate = useNavigate();

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
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={4} />
      </div>
    );
  }

  if (error) {
    return <ErrorState description={error.message} onRetry={() => void refetch()} />;
  }

  return (
    <EnrollmentTokensPage
      tokens={tokens}
      onCreateToken={handleCreate}
      onRevoke={handleRevoke}
      onViewAttempts={() => void navigate({ to: "/enrollment-attempts" })}
    />
  );
}
