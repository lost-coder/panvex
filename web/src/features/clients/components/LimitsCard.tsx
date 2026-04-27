// Q5.U-Q-08: extracted from ClientDetailPage.tsx (limits + metadata
// card). Reads only from the client-level payload; no shared state
// with the page so a clean component move was sufficient.
import { Badge, KvGrid, MonoValue, formatQuota } from "@/ui";
import type { ClientDetailPageProps } from "@/shared/api/types-pages/pages";

export function LimitsCard({ client }: Readonly<{ client: ClientDetailPageProps["client"] }>) {
  return (
    <section className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-3">
      <span className="text-sm font-semibold text-fg">Limits & metadata</span>
      <KvGrid
        rows={[
          {
            label: "Ad tag",
            value: client.userAdTag ? (
              <MonoValue>{client.userAdTag}</MonoValue>
            ) : (
              <span className="text-xs text-fg-faint">—</span>
            ),
          },
          {
            label: "Max TCP conns",
            value: (
              <MonoValue>
                {client.maxTcpConns > 0 ? client.maxTcpConns : "Unlimited"}
              </MonoValue>
            ),
          },
          {
            label: "Max unique IPs",
            value: (
              <MonoValue>
                {client.maxUniqueIps > 0 ? client.maxUniqueIps : "Unlimited"}
              </MonoValue>
            ),
          },
          {
            label: "Quota",
            value: <MonoValue>{formatQuota(client.dataQuotaBytes)}</MonoValue>,
          },
          {
            label: "Fleet groups",
            value:
              client.fleetGroupIds.length === 0 ? (
                <span className="text-xs text-fg-faint">—</span>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {client.fleetGroupIds.map((g) => (
                    <Badge key={g} variant="default">
                      {g}
                    </Badge>
                  ))}
                </div>
              ),
          },
        ]}
      />
    </section>
  );
}
