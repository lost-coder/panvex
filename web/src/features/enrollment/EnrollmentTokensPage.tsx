import { PageHeader } from "@/ui/layout/PageHeader";
import { Button } from "@/ui/base/button";
import { EmptyState } from "@/ui/components/EmptyState";
import { TokenList } from "@/features/enrollment/TokenList";
import type { EnrollmentTokensPageProps } from "@/shared/api/types-pages/pages";

export function EnrollmentTokensPage({
  tokens,
  onCreateToken,
  onRevoke,
}: EnrollmentTokensPageProps) {
  return (
    <div className="flex flex-col">
      <PageHeader
        title="Enrollment Tokens"
        subtitle={`${tokens.length} token${tokens.length !== 1 ? "s" : ""}`}
        trailing={
          <Button size="sm" onClick={onCreateToken}>
            + New Token
          </Button>
        }
      />

      <div className="px-4 md:px-8 pb-8">
        {tokens.length === 0 ? (
          <EmptyState
            title="No enrollment tokens"
            description="Generate a token to onboard a new Panvex agent. Each token is single-use and expires after its TTL."
            action={
              <Button size="sm" onClick={onCreateToken}>
                + New Token
              </Button>
            }
          />
        ) : (
          <TokenList tokens={tokens} onRevoke={onRevoke} />
        )}
      </div>
    </div>
  );
}
