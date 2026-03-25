import type { ServerDetailViewModel } from "./server-detail-view-model";

export function ServerDetailEventsPanel({
  items,
}: {
  items: ServerDetailViewModel["recentEventItems"];
}) {
  return (
    <section className="server-detail-events-panel server-detail-surface">
      <div className="server-detail-panel__head">
        <div>
          <div className="server-detail-panel__eyebrow">Runtime feed</div>
          <div className="server-detail-panel__title">Recent operational signals</div>
          <div className="server-detail-panel__subtitle">
            This feed stays intentionally compact so the latest degradation and recovery signals are
            visible without burying the rest of the page.
          </div>
        </div>
        <span className="server-detail-panel__kicker">{items.length} items</span>
      </div>
      <div className="server-detail-panel__body">
        {items.length > 0 ? (
          <div className="server-detail-events-panel__list">
            {items.map((item) => (
              <article
                className="server-detail-events-panel__item"
                data-tone={item.status}
                key={item.id}
              >
                <span className="server-detail-events-panel__dot" />
                <div className="server-detail-events-panel__text">{item.text}</div>
                <div className="server-detail-events-panel__time">{item.time}</div>
              </article>
            ))}
          </div>
        ) : (
          <div className="server-detail-empty">No runtime events reported.</div>
        )}
      </div>
    </section>
  );
}
