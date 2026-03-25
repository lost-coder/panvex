import { Copy } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { ClientDetailViewModel } from "./client-detail-view-model";

export function ClientDetailDeploymentTable({
  rows,
}: {
  rows: ClientDetailViewModel["deploymentRows"];
}) {
  return (
    <section className="client-detail-deployment-table client-detail-surface">
      <div className="client-detail-panel__head">
        <div>
          <div className="client-detail-panel__eyebrow">Per-node rollout</div>
          <div className="client-detail-panel__title">Deployment status and connection links</div>
          <div className="client-detail-panel__subtitle">
            This is the primary operational block for the client, showing which nodes applied the
            latest state and which ones still need attention.
          </div>
        </div>
        <span className="client-detail-panel__kicker">{rows.length} targets</span>
      </div>
      {rows.length > 0 ? (
        <div className="client-detail-table-wrap">
          <table className="client-detail-table">
            <thead>
              <tr>
                <th>Node</th>
                <th>Status</th>
                <th>Operation</th>
                <th>Last Applied</th>
                <th>Connection Link</th>
                <th>Last Error</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <td className="client-detail-table__strong">{row.agentText}</td>
                  <td>
                    <Badge dot size="sm" variant={row.statusTone}>
                      {row.statusText}
                    </Badge>
                  </td>
                  <td>{row.desiredOperationText}</td>
                  <td>{row.lastAppliedText}</td>
                  <td>
                    <div className="client-detail-deployment-table__link-cell">
                      <span className={row.linkText === "—" ? "client-detail-table__muted" : "client-detail-table__mono"}>
                        {row.linkText}
                      </span>
                      {row.linkText !== "—" && (
                        <button
                          className="client-detail-icon-button"
                          onClick={() => void navigator.clipboard.writeText(row.linkText)}
                          type="button"
                        >
                          <Copy className="h-4 w-4" />
                        </button>
                      )}
                    </div>
                  </td>
                  <td className={row.errorText === "—" ? "client-detail-table__muted" : "client-detail-table__tone-bad"}>
                    {row.errorText}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="client-detail-empty">No rollout targets configured.</div>
      )}
    </section>
  );
}
