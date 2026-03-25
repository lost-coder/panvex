import { Badge } from "@/components/ui/badge";
import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailUpstreamsTable({
  summaryText,
  rows,
}: {
  summaryText: ServerDetailViewModel["upstreamSummaryText"];
  rows: ServerDetailViewModel["upstreamRows"];
}) {
  return (
    <section className="server-detail-upstreams-table server-detail-surface">
      <div className="server-detail-panel__head">
        <div>
          <div className="server-detail-panel__eyebrow">Fallback reachability</div>
          <div className="server-detail-panel__title">Route quality and upstream health</div>
          <div className="server-detail-panel__subtitle">
            This table keeps slow or failing routes visible next to their addresses, which makes
            degraded fallback paths easier to correlate with event spikes.
          </div>
        </div>
        <span className="server-detail-panel__kicker">{summaryText}</span>
      </div>
      {rows.length > 0 ? (
        <div className="server-detail-table-wrap">
          <table className="server-detail-table">
            <thead>
              <tr>
                <th>Route Kind</th>
                <th>Address</th>
                <th>Health</th>
                <th>Fails</th>
                <th>Latency</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <td>{row.routeKindText}</td>
                  <td className="server-detail-table__mono">{row.addressText}</td>
                  <td>
                    <Badge dot size="sm" variant={row.healthTone}>
                      {row.healthText}
                    </Badge>
                  </td>
                  <td>{row.failsText}</td>
                  <td className="server-detail-table__tone" data-tone={row.healthTone}>
                    {row.latencyText}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="server-detail-empty">No upstream runtime data reported.</div>
      )}
    </section>
  );
}
