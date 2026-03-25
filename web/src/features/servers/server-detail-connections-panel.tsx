import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailConnectionsPanel({
  stats,
  meta,
}: {
  stats: ServerDetailViewModel["connectionStats"];
  meta: ServerDetailViewModel["connectionMeta"];
}) {
  return (
    <section className="server-detail-connections-panel server-detail-surface">
      <div className="server-detail-panel__head">
        <div>
          <div className="server-detail-panel__eyebrow">Latest reported snapshot</div>
          <div className="server-detail-panel__title">Current sessions and lifetime counters</div>
          <div className="server-detail-panel__subtitle">
            The top cards show what is active right now, while the lower counters keep the broader
            session error profile visible without crowding the hero.
          </div>
        </div>
        <span className="server-detail-panel__kicker">Reported totals</span>
      </div>
      <div className="server-detail-panel__body server-detail-connections-panel__body">
        <div className="server-detail-connections-panel__stats">
          {stats.map((stat) => (
            <article className="server-detail-connections-panel__card" key={stat.label}>
              <div className="server-detail-connections-panel__card-label">{stat.label}</div>
              <div className="server-detail-connections-panel__card-value">{stat.valueText}</div>
              <div className="server-detail-connections-panel__card-secondary">
                {stat.secondaryText}
              </div>
            </article>
          ))}
        </div>
        <div className="server-detail-connections-panel__meta">
          {meta.map((item) => (
            <div className="server-detail-connections-panel__meta-item" key={item.label}>
              <span className="server-detail-connections-panel__meta-label">{item.label}</span>
              <span className="server-detail-connections-panel__meta-value">{item.valueText}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
