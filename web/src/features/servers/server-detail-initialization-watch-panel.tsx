import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailInitializationWatchPanel({
  watch,
}: {
  watch: ServerDetailViewModel["initializationWatch"];
}) {
  if (!watch.visible) {
    return null;
  }

  return (
    <section className="server-detail-runtime-panel server-detail-surface">
      <div className="server-detail-panel__head">
        <div>
          <div className="server-detail-panel__eyebrow">Startup tracker</div>
          <div className="server-detail-panel__title">{watch.titleText}</div>
          <div className="server-detail-panel__subtitle">{watch.summaryText}</div>
        </div>
        <span className="server-detail-panel__kicker">{watch.badgeText}</span>
      </div>
      <div className="server-detail-panel__body">
        <div className="server-detail-runtime-panel__progress-grid">
          {watch.cards.map((card) => (
            <article className="server-detail-runtime-panel__card" key={card.label}>
              <div className="server-detail-runtime-panel__card-label">{card.label}</div>
              <div className="server-detail-runtime-panel__card-value">{card.valueText}</div>
              <div className="server-detail-runtime-panel__card-secondary">{card.secondaryText}</div>
              <div className="server-detail-runtime-panel__progress-track">
                <div
                  className="server-detail-runtime-panel__progress-bar"
                  style={{ width: `${Math.max(0, Math.min(card.progressPct, 100))}%` }}
                />
              </div>
              <div className="server-detail-runtime-panel__progress-value">{card.progressPct}%</div>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
