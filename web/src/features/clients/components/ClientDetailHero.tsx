// R-Q-08: hero band + mobile page-header + actions strip extracted
// from ClientDetailPage.tsx. The layout twins (mobile PageHeader vs
// desktop HeroStrip) live together so a designer changing the action
// list updates one file instead of two parallel JSX sections.

import {
  Button,
  HeroStrip,
  PageHeader,
  type HeroMetaPill,
  type StatusTone,
} from "@/ui";

import { expiresSuffix, expiresTone } from "./clientDetailHelpers";

export interface ClientDetailHeroProps {
  name: string;
  enabled: boolean;
  expirationRfc3339: string;
  fleetGroupIds: string[];
  statusLabel: string;
  status: "active" | "disabled" | "expired";
  hasFailedDeployment: boolean;
  onEdit?: (() => void) | undefined;
  onDisable?: (() => void) | undefined;
  onRedeploy?: (() => void) | undefined;
  redeploying?: boolean | undefined;
  onDelete?: (() => void) | undefined;
}

export function ClientDetailHero({
  name,
  enabled,
  expirationRfc3339,
  fleetGroupIds,
  statusLabel,
  status,
  hasFailedDeployment,
  onEdit,
  onDisable,
  onRedeploy,
  redeploying,
  onDelete,
}: ClientDetailHeroProps) {
  const actions = (
    <>
      {onEdit && (
        <Button size="sm" variant="outline" onClick={onEdit}>
          Edit
        </Button>
      )}
      {onDisable && (
        <Button size="sm" variant="ghost" onClick={onDisable}>
          {enabled ? "Disable" : "Enable"}
        </Button>
      )}
      {hasFailedDeployment && onRedeploy && (
        <Button
          size="sm"
          variant="outline"
          onClick={onRedeploy}
          disabled={redeploying}
          className="text-status-warn border-status-warn/60 hover:text-status-warn"
          title="Re-run the client rollout to every target node"
        >
          {redeploying ? "Redeploying…" : "Redeploy"}
        </Button>
      )}
      {onDelete && (
        <Button
          size="sm"
          variant="ghost"
          onClick={onDelete}
          className="text-status-error hover:text-status-error"
        >
          Delete
        </Button>
      )}
    </>
  );
  return (
    <>
      {/* Mobile — PageHeader carries name + status subtitle. */}
      <div className="md:hidden">
        <PageHeader
          title={name}
          subtitle={`${statusLabel.toLowerCase()} · ${expiresSuffix(expirationRfc3339)}`}
          trailing={
            onEdit ? (
              <Button size="sm" onClick={onEdit}>
                Edit
              </Button>
            ) : undefined
          }
        />
      </div>

      {/* Desktop hero — full-bleed band, matches the Server detail style. */}
      <HeroStrip
        className="hidden md:flex"
        name={name}
        status={{
          tone: status === "expired" ? "error" : enabled ? "ok" : "warn",
          label: statusLabel,
        }}
        pills={[
          ...fleetGroupIds.map<HeroMetaPill>((g) => ({
            label: "group",
            value: g,
            mono: true,
          })),
          {
            label: "expires",
            value: expiresSuffix(expirationRfc3339),
            mono: true,
            tone: expiresTone(expirationRfc3339) as StatusTone,
          },
        ]}
        actions={actions}
      />
    </>
  );
}
