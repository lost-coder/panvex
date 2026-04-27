import {
  AgeCell,
  Button,
  CopyButton,
  DataTable,
  EmptyState,
  StatusLabel,
  cn,
  formatTime,
  type StatusTone,
} from "@/ui";
import type { TokenListProps, EnrollmentTokenData } from "@/shared/api/types-pages/pages";

const statusTone: Record<EnrollmentTokenData["status"], StatusTone> = {
  active: "ok",
  consumed: "default",
  expired: "warn",
  revoked: "error",
};

function TokenCell({ value }: Readonly<{ value: string }>) {
  return (
    <div className="flex items-center gap-2 min-w-0">
      <span className="font-mono text-xs text-fg break-all">{value}</span>
      <CopyButton text={value} />
    </div>
  );
}

export function TokenList({ tokens, onRevoke, nowSec }: Readonly<TokenListProps>) {
  const columns = [
    {
      key: "value",
      header: "Token",
      render: (t: Readonly<EnrollmentTokenData>) => <TokenCell value={t.value} />,
      className: "min-w-[200px]",
    },
    {
      key: "fleetGroup",
      header: "Fleet Group",
      render: (t: Readonly<EnrollmentTokenData>) => (
        <span
          className={cn(
            "text-sm",
            t.fleetGroupId ? "text-fg font-mono" : "text-fg-faint italic",
          )}
        >
          {t.fleetGroupId || "default scope"}
        </span>
      ),
      className: "hidden md:table-cell w-[160px]",
    },
    {
      key: "status",
      header: "Status",
      render: (t: Readonly<EnrollmentTokenData>) => (
        <StatusLabel tone={statusTone[t.status]} label={t.status} />
      ),
      className: "w-[120px]",
    },
    {
      key: "issued",
      header: "Issued",
      render: (t: Readonly<EnrollmentTokenData>) => (
        <span className="text-[11px] font-mono text-fg-muted tabular-nums">
          {formatTime(t.issuedAtUnix)}
        </span>
      ),
      className: "hidden sm:table-cell w-[100px]",
    },
    {
      key: "expires",
      header: "Expires",
      // Countdown is meaningful only for active tokens; past that the column
      // falls back to a plain absolute time.
      render: (t: Readonly<EnrollmentTokenData>) =>
        t.status === "active" ? (
          <AgeCell unixSec={t.expiresAtUnix} mode="expires" nowSec={nowSec} />
        ) : (
          <span className="text-[11px] font-mono text-fg-muted tabular-nums">
            {formatTime(t.expiresAtUnix)}
          </span>
        ),
      className: "text-right w-[120px]",
    },
    {
      key: "actions",
      header: "",
      render: (t: Readonly<EnrollmentTokenData>) =>
        t.status === "active" ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onRevoke(t.value)}
            className="text-status-error hover:text-status-error"
          >
            Revoke
          </Button>
        ) : null,
      className: "text-right",
    },
  ];

  if (tokens.length === 0) {
    return (
      <EmptyState
        title="No enrollment tokens"
        description="No active tokens match the current filter."
      />
    );
  }

  return <DataTable data={tokens} columns={columns} keyExtractor={(t) => t.value} />;
}
