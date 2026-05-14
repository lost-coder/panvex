import { useTranslation } from "react-i18next";

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
  const { t } = useTranslation("enrollment");
  const columns = [
    {
      key: "value",
      header: t("tokens.table.token"),
      render: (tok: Readonly<EnrollmentTokenData>) => <TokenCell value={tok.value} />,
      className: "min-w-[200px]",
    },
    {
      key: "fleetGroup",
      header: t("tokens.table.fleetGroup"),
      render: (tok: Readonly<EnrollmentTokenData>) => (
        <span
          className={cn(
            "text-sm",
            tok.fleetGroupId ? "text-fg font-mono" : "text-fg-faint italic",
          )}
        >
          {tok.fleetGroupId || t("tokens.table.defaultScope")}
        </span>
      ),
      className: "hidden md:table-cell w-[160px]",
    },
    {
      key: "status",
      header: t("tokens.table.status"),
      render: (tok: Readonly<EnrollmentTokenData>) => (
        <StatusLabel tone={statusTone[tok.status]} label={tok.status} />
      ),
      className: "w-[120px]",
    },
    {
      key: "issued",
      header: t("tokens.table.issued"),
      render: (tok: Readonly<EnrollmentTokenData>) => (
        <span className="text-[11px] font-mono text-fg-muted tabular-nums">
          {formatTime(tok.issuedAtUnix)}
        </span>
      ),
      className: "hidden sm:table-cell w-[100px]",
    },
    {
      key: "expires",
      header: t("tokens.table.expires"),
      // Countdown is meaningful only for active tokens; past that the column
      // falls back to a plain absolute time.
      render: (tok: Readonly<EnrollmentTokenData>) =>
        tok.status === "active" ? (
          <AgeCell unixSec={tok.expiresAtUnix} mode="expires" nowSec={nowSec} />
        ) : (
          <span className="text-[11px] font-mono text-fg-muted tabular-nums">
            {formatTime(tok.expiresAtUnix)}
          </span>
        ),
      className: "text-right w-[120px]",
    },
    {
      key: "actions",
      header: "",
      render: (tok: Readonly<EnrollmentTokenData>) =>
        tok.status === "active" ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onRevoke(tok.value)}
            className="text-status-error hover:text-status-error"
          >
            {t("tokens.table.revoke")}
          </Button>
        ) : null,
      className: "text-right",
    },
  ];

  if (tokens.length === 0) {
    return (
      <EmptyState
        title={t("tokens.empty.listTitle")}
        description={t("tokens.empty.listDescription")}
      />
    );
  }

  return <DataTable data={tokens} columns={columns} keyExtractor={(tok) => tok.value} />;
}
