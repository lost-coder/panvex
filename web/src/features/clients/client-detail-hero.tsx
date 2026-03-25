import { ArrowLeft } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { ClientDetailViewModel } from "./client-detail-view-model";

export function ClientDetailHero({
  header,
  onBack,
}: {
  header: ClientDetailViewModel["header"];
  onBack: () => void;
}) {
  return (
    <section className="client-detail-hero client-detail-surface">
      <button className="client-detail-hero__back" onClick={onBack} type="button">
        <ArrowLeft className="h-4 w-4" />
        Back to Clients
      </button>
      <div className="client-detail-hero__body">
        <div className="client-detail-hero__content">
          <div className="client-detail-hero__eyebrow">Managed client snapshot</div>
          <div className="client-detail-hero__title-row">
            <h1 className="client-detail-hero__title">{header.nameText}</h1>
            <Badge dot variant={header.statusTone}>
              {header.statusText}
            </Badge>
            <Badge dot variant={header.deploymentTone}>
              {header.deploymentText}
            </Badge>
          </div>
          <p className="client-detail-hero__summary">
            Identity, usage, rollout targets, and per-node deployment status for the latest
            reported control-plane snapshot of this client.
          </p>
        </div>
        <dl className="client-detail-hero__meta">
          {header.metaItems.map((item) => (
            <div className="client-detail-hero__meta-pill" key={item.label}>
              <dt>{item.label}</dt>
              <dd>{item.valueText}</dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}
