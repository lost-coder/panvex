import type { ClientDetailViewModel } from "./client-detail-view-model";

export function ClientDetailAssignmentsPanel({
  summaryText,
  groups,
  agents,
}: {
  summaryText: ClientDetailViewModel["assignmentSummaryText"];
  groups: ClientDetailViewModel["assignmentGroups"];
  agents: ClientDetailViewModel["assignmentAgents"];
}) {
  return (
    <section className="client-detail-assignments-panel client-detail-surface">
      <PanelHeader
        eyebrow="Rollout rules"
        title="Fleet groups and explicit nodes"
        subtitle="Assignments stay visible separately from limits so rollout scope can be audited at a glance."
      />
      <div className="client-detail-panel__body client-detail-assignments-panel__body">
        <div className="client-detail-assignments-panel__summary">{summaryText}</div>
        <div className="client-detail-assignments-panel__columns">
          <AssignmentList
            emptyText="No fleet groups selected"
            items={groups}
            title="Fleet groups"
          />
          <AssignmentList
            emptyText="No explicit nodes selected"
            items={agents}
            title="Explicit nodes"
          />
        </div>
      </div>
    </section>
  );
}

function AssignmentList({
  title,
  items,
  emptyText,
}: {
  title: string;
  items: string[];
  emptyText: string;
}) {
  return (
    <div className="client-detail-assignments-panel__list">
      <div className="client-detail-assignments-panel__list-title">{title}</div>
      {items.length > 0 ? (
        <div className="client-detail-assignments-panel__tags">
          {items.map((item) => (
            <span className="client-detail-assignments-panel__tag" key={item}>
              {item}
            </span>
          ))}
        </div>
      ) : (
        <div className="client-detail-assignments-panel__empty">{emptyText}</div>
      )}
    </div>
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
