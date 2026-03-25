import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailKpis({
  stats,
}: {
  stats: ServerDetailViewModel["overviewStats"];
}) {
  return (
    <section className="server-detail-kpis">
      {stats.map((stat) => (
        <article className="server-detail-kpi server-detail-surface" key={stat.label}>
          <div className="server-detail-kpi__label">{stat.label}</div>
          <div className="server-detail-kpi__value">{stat.valueText}</div>
          <div className="server-detail-kpi__secondary">{stat.secondaryText}</div>
        </article>
      ))}
    </section>
  );
}
