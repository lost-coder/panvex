// src/compositions/TokenList.tsx
import { Badge } from "@/ui/primitives/Badge";
import { Button } from "@/ui/base/button";
import { CopyButton } from "@/ui/primitives/CopyButton";
import { DataTable } from "@/ui/components/DataTable";
import { tokenStatusVariant } from "@/ui/lib/status";
import type { TokenListProps, EnrollmentTokenData } from "@/shared/api/types-pages/pages";
import { formatTime } from "@/ui";

export function TokenList({ tokens, onRevoke }: TokenListProps) {
  const columns = [
    {
      key: "value",
      header: "Token",
      render: (t: EnrollmentTokenData) => (
        // Phase-7 fix: the token has to be readable and copyable — operators
        // paste it into the agent bootstrap command. Previously it was
        // truncated after 16 chars AND rendered in a muted color inherited
        // from the parent, so neither use was possible.
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-xs text-fg break-all">{t.value}</span>
          <CopyButton text={t.value} />
        </div>
      ),
    },
    {
      key: "fleetGroup",
      header: "Fleet Group",
      render: (t: EnrollmentTokenData) => <span className="text-sm text-fg">{t.fleetGroupId}</span>,
    },
    {
      key: "status",
      header: "Status",
      render: (t: EnrollmentTokenData) => (
        <Badge variant={tokenStatusVariant[t.status] ?? "default"}>{t.status}</Badge>
      ),
    },
    {
      key: "issued",
      header: "Issued",
      render: (t: EnrollmentTokenData) => (
        <span className="text-xs text-fg-muted">{formatTime(t.issuedAtUnix)}</span>
      ),
    },
    {
      key: "expires",
      header: "Expires",
      render: (t: EnrollmentTokenData) => (
        <span className="text-xs text-fg-muted">{formatTime(t.expiresAtUnix)}</span>
      ),
    },
    {
      key: "actions",
      header: "",
      render: (t: EnrollmentTokenData) =>
        t.status === "active" ? (
          <Button variant="ghost" size="sm" onClick={() => onRevoke(t.value)}>
            Revoke
          </Button>
        ) : null,
    },
  ];

  if (tokens.length === 0) {
    return (
      <div className="text-center text-sm text-fg-muted py-8">
        No enrollment tokens created yet.
      </div>
    );
  }

  return <DataTable data={tokens} columns={columns} keyExtractor={(t) => t.value} />;
}
