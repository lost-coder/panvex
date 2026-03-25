import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailRuntimePanel({
  progressCards,
  flags,
}: {
  progressCards: ServerDetailViewModel["runtimeProgressCards"];
  flags: ServerDetailViewModel["runtimeFlags"];
}) {
  return (
    <section className="server-detail-runtime-panel server-detail-surface">
      <div className="server-detail-panel__head">
        <div>
          <div className="server-detail-panel__eyebrow">Readiness and gates</div>
          <div className="server-detail-panel__title">Bootstrap progress and admission state</div>
          <div className="server-detail-panel__subtitle">
            Startup and initialization stages stay separated here so readiness regressions are easy
            to distinguish from connection gating.
          </div>
        </div>
        <span className="server-detail-panel__kicker">Primary health context</span>
      </div>
      <div className="server-detail-panel__body">
        <div className="server-detail-runtime-panel__grid">
          <div className="server-detail-runtime-panel__progress-grid">
            {progressCards.map((card) => (
              <article className="server-detail-runtime-panel__card" key={card.label}>
                <div className="server-detail-runtime-panel__card-label">{card.label}</div>
                <div className="server-detail-runtime-panel__card-value">{card.valueText}</div>
                <div className="server-detail-runtime-panel__card-secondary">
                  {card.secondaryText}
                </div>
                <div className="server-detail-runtime-panel__progress-track">
                  <div
                    className="server-detail-runtime-panel__progress-bar"
                    style={{ width: `${Math.max(0, Math.min(card.progressPct, 100))}%` }}
                  />
                </div>
                <div className="server-detail-runtime-panel__progress-value">
                  {card.progressPct}%
                </div>
              </article>
            ))}
          </div>
          <div className="server-detail-runtime-panel__flag-grid">
            {flags.map((flag) => (
              <article className="server-detail-runtime-panel__card" key={flag.label}>
                <div className="server-detail-runtime-panel__card-label">{flag.label}</div>
                <div className="server-detail-runtime-panel__card-value">{flag.valueText}</div>
                <div className="server-detail-runtime-panel__card-secondary">
                  {flag.secondaryText}
                </div>
              </article>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
