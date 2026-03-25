import type { ClientDetailViewModel } from "./client-detail-view-model";

export function ClientDetailLimitsPanel({
  items,
}: {
  items: ClientDetailViewModel["limitItems"];
}) {
  return (
    <section className="client-detail-limits-panel client-detail-surface">
      <PanelHeader
        eyebrow="Applied limits"
        title="Connection and quota constraints"
        subtitle="These values reflect the central limits Panvex is distributing to Telemt nodes for this client."
      />
      <div className="client-detail-panel__body">
        <div className="client-detail-simple-list">
          {items.map((item) => (
            <div className="client-detail-simple-list__item" key={item.label}>
              <span className="client-detail-simple-list__label">{item.label}</span>
              <span className="client-detail-simple-list__value">{item.valueText}</span>
            </div>
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
