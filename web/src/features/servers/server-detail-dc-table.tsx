import { Badge } from "@/components/ui/badge";
import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailDcTable({
  rows,
}: {
  rows: ServerDetailViewModel["dcRows"];
}) {
  return (
    <section className="server-detail-dc-table server-detail-surface">
      <div className="server-detail-panel__head">
        <div>
          <div className="server-detail-panel__eyebrow">Primary health context</div>
          <div className="server-detail-panel__title">Coverage and writer health by DC</div>
          <div className="server-detail-panel__subtitle">
            Use this block to spot quorum drops, high RTT, and local writer starvation before
            drilling deeper into route failures.
          </div>
        </div>
        <span className="server-detail-panel__kicker">{rows.length} rows</span>
      </div>
      {rows.length > 0 ? (
        <div className="server-detail-table-wrap">
          <table className="server-detail-table">
            <thead>
              <tr>
                <th>DC</th>
                <th>Status</th>
                <th>RTT</th>
                <th>Coverage</th>
                <th>Writers</th>
                <th>Endpoints</th>
                <th>Load</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <td className="server-detail-table__mono">{row.dcText}</td>
                  <td>
                    <Badge dot size="sm" variant={row.statusTone}>
                      {row.statusText}
                    </Badge>
                  </td>
                  <td className="server-detail-table__tone" data-tone={row.statusTone}>
                    {row.rttText}
                  </td>
                  <td className="server-detail-table__tone" data-tone={row.statusTone}>
                    {row.coverageText}
                  </td>
                  <td>{row.writersText}</td>
                  <td>{row.endpointsText}</td>
                  <td className="server-detail-table__strong">{row.loadText}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="server-detail-empty">No DC runtime data reported.</div>
      )}
    </section>
  );
}
