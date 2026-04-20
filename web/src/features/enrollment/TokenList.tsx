// src/compositions/TokenList.tsx
import { Badge } from "@/ui/primitives/Badge";
import { Button } from "@/ui/base/button";
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
        <span className="font-mono text-xs">{t.value.slice(0, 16)}...</span>
      ),
    },
    {
      key: "fleetGroup",
      header: "Fleet Group",
      render: (t: EnrollmentTokenData) => <span className="text-sm">{t.fleetGroupId}</span>,
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
