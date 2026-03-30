import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailRuntimePanel({
  flags,
}: {
  flags: ServerDetailViewModel["runtimeFlags"];
}) {
  return (
    <section className="server-detail-runtime-panel server-detail-surface">
      <div className="server-detail-panel__head">
        <div>
          <div className="server-detail-panel__eyebrow">Runtime gates</div>
          <div className="server-detail-panel__title">Admission & Runtime Gates</div>
          <div className="server-detail-panel__subtitle">
            These gates stay visible after startup because admission and ME readiness can degrade
            later during normal operation.
          </div>
        </div>
        <span className="server-detail-panel__kicker">Always visible</span>
      </div>
      <div className="server-detail-panel__body">
        <div className="server-detail-runtime-panel__grid server-detail-runtime-panel__grid--flags-only">
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
