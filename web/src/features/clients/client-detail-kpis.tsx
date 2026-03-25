import type { ClientDetailViewModel } from "./client-detail-view-model";

export function ClientDetailKpis({
  stats,
}: {
  stats: ClientDetailViewModel["overviewStats"];
}) {
  return (
    <section className="client-detail-kpis">
      {stats.map((stat) => (
        <article className="client-detail-kpi client-detail-surface" key={stat.label}>
          <div className="client-detail-kpi__label">{stat.label}</div>
          <div className="client-detail-kpi__value">{stat.valueText}</div>
          <div className="client-detail-kpi__secondary">{stat.secondaryText}</div>
        </article>
      ))}
    </section>
  );
}
