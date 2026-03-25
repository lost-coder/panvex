import { useEffect, useState } from "react";
import { Copy, Eye, EyeOff } from "lucide-react";
import type { ClientDetailViewModel } from "./client-detail-view-model";

export function ClientDetailIdentityPanel({
  items,
  panelKey,
  secret,
}: {
  items: ClientDetailViewModel["identityItems"];
  panelKey: string;
  secret: ClientDetailViewModel["identitySecret"];
}) {
  const [revealed, setRevealed] = useState(false);

  useEffect(() => {
    setRevealed(false);
  }, [panelKey]);

  return (
    <section className="client-detail-identity-panel client-detail-surface">
      <PanelHeader
        eyebrow="Central identity"
        title="Name, tags, and secret"
        subtitle="The secret stays masked by default so you can inspect identity details without exposing credentials."
      />
      <div className="client-detail-panel__body client-detail-identity-panel__body">
        <div className="client-detail-identity-panel__list">
          {items.map((item) => (
            <div className="client-detail-identity-panel__item" key={item.label}>
              <span className="client-detail-identity-panel__label">{item.label}</span>
              <span className="client-detail-identity-panel__value">{item.valueText}</span>
            </div>
          ))}
        </div>
        <div className="client-detail-identity-panel__secret">
          <div className="client-detail-identity-panel__secret-head">
            <div>
              <div className="client-detail-identity-panel__secret-label">Secret</div>
              <div className="client-detail-identity-panel__secret-value">
                {revealed ? secret.revealedText : secret.maskedText}
              </div>
            </div>
            <div className="client-detail-identity-panel__secret-actions">
              <button
                className="client-detail-action-button"
                onClick={() => setRevealed((currentValue) => !currentValue)}
                type="button"
              >
                {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                {revealed ? "Hide" : "Reveal"}
              </button>
              <button
                className="client-detail-action-button"
                onClick={() => void navigator.clipboard.writeText(secret.revealedText)}
                type="button"
              >
                <Copy className="h-4 w-4" />
                Copy
              </button>
            </div>
          </div>
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
