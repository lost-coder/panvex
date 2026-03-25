import type { ClientDetailViewModel } from "./client-detail-view-model";

export function ClientDetailUsagePanel({
  items,
}: {
  items: ClientDetailViewModel["usageItems"];
}) {
  return (
    <section className="client-detail-usage-panel client-detail-surface">
      <PanelHeader
        eyebrow="Current usage"
        title="Aggregated traffic and session footprint"
        subtitle="This panel stays focused on the live usage counters that matter most during day-to-day operations."
      />
      <div className="client-detail-panel__body">
        <div className="client-detail-usage-panel__grid">
          {items.map((item) => (
            <article className="client-detail-usage-panel__card" key={item.label}>
              <div className="client-detail-usage-panel__label">{item.label}</div>
              <div className="client-detail-usage-panel__value">{item.valueText}</div>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function PanelHeader({
  eyebrow,
  title,
  subtitle,
}: {
  eyebrow: string;
  title: string;
  subtitle: string;
}) {
  return (
    <div className="client-detail-panel__head">
      <div>
        <div className="client-detail-panel__eyebrow">{eyebrow}</div>
        <div className="client-detail-panel__title">{title}</div>
        <div className="client-detail-panel__subtitle">{subtitle}</div>
      </div>
    </div>
  );
}
