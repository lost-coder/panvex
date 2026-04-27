import { useState, useEffect, useCallback } from "react";
import { EnrollmentWizard } from "@/features/enrollment/EnrollmentWizard";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";
import { useFleetGroups } from "./hooks/useFleetGroups";
import { useFleetGroupMutations } from "@/features/fleet-groups/hooks/useFleetGroupsFull";
import { FleetGroupFormSheet, type FleetGroupFormData } from "@/features/fleet-groups/FleetGroupFormSheet";
import { Sheet, SheetBody, SheetContent } from "@/ui";
import { useToast } from "@/app/providers/ToastProvider";
import { useNavigate } from "@tanstack/react-router";
import { apiClient } from "@/shared/api/api";
import type { EnrollmentTokenResponse, Agent } from "@/shared/api/api";
import {
  DEFAULT_TELEMT_METRICS_URL,
  DEFAULT_TELEMT_URL,
} from "@/shared/lib/defaults";
import { isValidNodeName } from "@/shared/lib/shell-quote";
import { buildInstallCommand } from "./install-command";

export function AddServerContainer() {
  const navigate = useNavigate();
  const toast = useToast();
  const { fleetGroups } = useFleetGroups();
  const { createMutation: createFleetGroupMutation } = useFleetGroupMutations();

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [nodeName, setNodeName] = useState("");
  const [selectedFleetGroup, setSelectedFleetGroup] = useState("");
  const [tokenTtl, setTokenTtl] = useState(3600);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [advancedOptions, setAdvancedOptions] = useState({
    telemtUrl: DEFAULT_TELEMT_URL,
    telemtMetricsUrl: DEFAULT_TELEMT_METRICS_URL,
    telemtAuth: "",
    insecureTransport: false,
  });

  const [tokenData, setTokenData] = useState<EnrollmentTokenResponse | null>(null);
  // Captured at token-generation time so the displayed countdown is a
  // pure function of render. Calling Date.now() during render is flagged
  // by react-hooks/purity (7.1.x) because the value drifts across renders.
  const [tokenExpiresInSecs, setTokenExpiresInSecs] = useState(0);

  const [connectionStatus, setConnectionStatus] = useState<
    EnrollmentWizardProps["connectionStatus"]
  >({
    bootstrap: "pending",
    grpcConnect: "pending",
    firstData: "pending",
  });
  const [connectedAgent, setConnectedAgent] = useState<
    EnrollmentWizardProps["connectedAgent"] | undefined
  >();

  useEffect(() => {
    const first = fleetGroups[0];
    if (first && !selectedFleetGroup) {
      // R-Q-24: seed the controlled select with the first fleet group on
      // first arrival. The condition guarantees the setter fires only
      // once per population, so the warning's "cascading render" concern
      // does not apply.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSelectedFleetGroup(first.id);
    }
  }, [fleetGroups, selectedFleetGroup]);

  const panelUrl = tokenData?.panel_url ?? "";
  const tokenValue = tokenData?.value ?? "";

  const installCommand = tokenData
    ? buildInstallCommand(panelUrl, tokenValue, nodeName, advancedOptions)
    : "";

  const handleGenerateToken = useCallback(async () => {
    // Validate before the network round-trip so the operator gets an
    // immediate, scoped error. The same predicate also keeps anything
    // shell-unsafe out of the rendered install command.
    if (!isValidNodeName(nodeName)) {
      setError(
        "Node name must be 1-64 chars: letters, digits, dot, dash, underscore.",
      );
      return;
    }
    setLoading(true);
    setError(undefined);
    try {
      const result = await apiClient.createEnrollmentToken({
        fleet_group_id: selectedFleetGroup,
        ttl_seconds: tokenTtl,
      });
      setTokenData(result);
      setTokenExpiresInSecs(
        Math.max(0, result.expires_at_unix - Math.floor(Date.now() / 1000)),
      );
      setStep(2);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create token");
    } finally {
      setLoading(false);
    }
  }, [selectedFleetGroup, tokenTtl, nodeName]);

  const handleInstallConfirm = useCallback(() => {
    // Bootstrap is the FIRST thing we wait on — the agent must
    // exchange its one-shot token for a certificate before anything
    // else happens. Gateway + first-data stages stay pending until
    // that lands.
    setConnectionStatus({
      bootstrap: "waiting",
      grpcConnect: "pending",
      firstData: "pending",
    });
    setStep(3);
  }, []);

  const goBack = useCallback(() => navigate({ to: "/servers" }), [navigate]);

  useEffect(() => {
    if (step !== 3 || !tokenValue) return;

    let cancelled = false;
    // After MAX_CONSECUTIVE_FAILURES probes in a row we surface the backend
    // error instead of silently polling forever. This protects against auth
    // lapses (401 after session expires), network loss, and schema drift.
    const MAX_CONSECUTIVE_FAILURES = 3;
    let consecutiveFailures = 0;

    // Apply the three-stage progression once we've picked an agent to
    // follow. Returns true when all three stages landed (caller should
    // stop polling).
    const applyAgentStatus = (match: Agent): boolean => {
      const online = match.presence_state === "online";
      const hasRuntime = Boolean(match.runtime);
      if (online && hasRuntime) {
        setConnectionStatus({
          bootstrap: "done",
          grpcConnect: "done",
          firstData: "done",
        });
        setConnectedAgent({
          id: match.id,
          version: match.version,
          fleetGroup: match.fleet_group_id || "default",
          certExpiresAt: match.cert_expires_at ?? "—",
        });
        return true;
      }
      setConnectionStatus({
        bootstrap: "done",
        grpcConnect: online ? "done" : "waiting",
        firstData: online ? "waiting" : "pending",
      });
      return false;
    };

    const poll = async () => {
      while (!cancelled) {
        await new Promise((r) => setTimeout(r, 3000));
        if (cancelled) break;
        try {
          const agents: Agent[] = await apiClient.agents();
          // Three-stage progression:
          //   1. Bootstrap  → token consumed in the backend
          //   2. Gateway    → agent record appears (presence != offline)
          //   3. First data → presence_state === "online" with runtime
          //                   telemetry attached
          const match = agents.find((a) => a.node_name === nodeName);
          if (match) {
            consecutiveFailures = 0;
            if (applyAgentStatus(match)) return;
          } else {
            const tokens = await apiClient.listEnrollmentTokens();
            const ourToken = tokens.find((t) => t.value === tokenValue);
            // Terminal token state — no agent record will ever appear with
            // an expired/revoked token. Surface the reason and stop
            // polling instead of leaving the operator stuck in "waiting".
            if (ourToken?.status === "expired" || ourToken?.status === "revoked") {
              setError(
                ourToken.status === "expired"
                  ? "Enrollment token expired before the agent dialed in. Generate a new token and re-run the install command."
                  : "Enrollment token was revoked. Generate a new token and re-run the install command.",
              );
              return;
            }
            if (ourToken?.status === "consumed" && ourToken.consumed_at_unix) {
              // Fallback: the agent didn't register under the node_name the
              // operator typed (install script fell back to hostname, the
              // flag was stripped from the paste, etc.). Token is consumed,
              // so SOME agent was registered from this token — find the
              // closest match by cert_issued_at within a 5-minute window
              // around the token's consumed_at. Picks the most-recent
              // cert-issue on tie so repeated bootstraps still resolve to
              // the latest.
              const consumedAt = ourToken.consumed_at_unix;
              const WINDOW_SECS = 300;
              const candidate = agents
                .filter((a) => a.cert_issued_at)
                .map((a) => {
                  const t = Date.parse(a.cert_issued_at!);
                  return Number.isFinite(t)
                    ? { a, issuedAt: Math.floor(t / 1000) }
                    : null;
                })
                .filter(
                  (x): x is { a: Agent; issuedAt: number } =>
                    x !== null && Math.abs(x.issuedAt - consumedAt) < WINDOW_SECS,
                )
                .sort((x, y) => y.issuedAt - x.issuedAt)[0];

              if (candidate) {
                if (applyAgentStatus(candidate.a)) return;
              } else {
                // Token consumed, no agent match yet → bootstrap done,
                // gateway still waiting.
                setConnectionStatus({
                  bootstrap: "done",
                  grpcConnect: "waiting",
                  firstData: "pending",
                });
              }
            }
            consecutiveFailures = 0;
          }
        } catch (err) {
          // Count up. If we keep failing, surface the last error so the
          // operator knows the backend is unreachable instead of waiting
          // forever. Transient glitches reset the counter as soon as a
          // probe succeeds.
          consecutiveFailures++;
          if (consecutiveFailures >= MAX_CONSECUTIVE_FAILURES) {
            setError(
              err instanceof Error
                ? `Probe failed ${consecutiveFailures}× in a row: ${err.message}`
                : `Probe failed ${consecutiveFailures}× in a row.`,
            );
            return;
          }
        }
      }
    };
    poll();
    return () => {
      cancelled = true;
    };
  }, [step, tokenValue, nodeName]);

  const fleetGroupOptions = fleetGroups.map((g) => ({
    id: g.id,
    name: g.label || g.name || g.id,
    nodeCount: g.agent_count,
  }));

  // Inline fleet-group quick-create so the wizard doesn't need a
  // round-trip to /fleet-groups. On success we auto-select the freshly
  // minted UUID so the operator's next click is "Generate token".
  const [quickCreateOpen, setQuickCreateOpen] = useState(false);
  const [quickCreateData, setQuickCreateData] = useState<FleetGroupFormData>({
    name: "",
    label: "",
    description: "",
  });
  const [quickCreateError, setQuickCreateError] = useState<string>("");

  const handleQuickCreateSubmit = async () => {
    setQuickCreateError("");
    try {
      const created = await createFleetGroupMutation.mutateAsync({
        name: quickCreateData.name,
        label: quickCreateData.label,
        description: quickCreateData.description,
      });
      toast.success(`Fleet group «${created.label}» created.`);
      setSelectedFleetGroup(created.id);
      setQuickCreateOpen(false);
      setQuickCreateData({ name: "", label: "", description: "" });
    } catch (err) {
      setQuickCreateError(err instanceof Error ? err.message : "Request failed");
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Add server wizard"
        className="relative bg-bg-card rounded-lg border border-border shadow-xl w-full max-w-[480px] max-h-[85vh] overflow-y-auto mx-4 p-6"
      >
        <button
          type="button"
          onClick={goBack}
          className="absolute top-3 right-3 text-fg-muted hover:text-fg text-lg leading-none"
          aria-label="Close"
        >
          ✕
        </button>
        <EnrollmentWizard
          step={step}
          fleetGroups={fleetGroupOptions}
          nodeName={nodeName}
          selectedFleetGroup={selectedFleetGroup}
          tokenTtl={tokenTtl}
          onNodeNameChange={setNodeName}
          onFleetGroupChange={setSelectedFleetGroup}
          onCreateFleetGroup={() => {
            setQuickCreateData({ name: "", label: "", description: "" });
            setQuickCreateError("");
            setQuickCreateOpen(true);
          }}
          onTokenTtlChange={setTokenTtl}
          onGenerateToken={handleGenerateToken}
          installCommand={installCommand}
          tokenValue={tokenValue}
          tokenExpiresInSecs={tokenExpiresInSecs}
          advancedOptions={advancedOptions}
          onAdvancedOptionsChange={setAdvancedOptions}
          onInstallConfirm={handleInstallConfirm}
          onBack={() => setStep(1)}
          connectionStatus={connectionStatus}
          connectedAgent={connectedAgent}
          onViewDetails={() => {
            if (connectedAgent) {
              navigate({
                to: "/servers/$serverId",
                params: { serverId: connectedAgent.id },
              });
            }
          }}
          onCancel={goBack}
          loading={loading}
          error={error}
        />
      </div>

      <Sheet
        open={quickCreateOpen}
        onOpenChange={(open) => { if (!open) setQuickCreateOpen(false); }}
      >
        <SheetContent
          side="bottom"
          title="New fleet group"
          onOpenChange={(open) => { if (!open) setQuickCreateOpen(false); }}
        >
          <SheetBody>
            <FleetGroupFormSheet
              mode="create"
              data={quickCreateData}
              onChange={setQuickCreateData}
              onSubmit={handleQuickCreateSubmit}
              onCancel={() => setQuickCreateOpen(false)}
              loading={createFleetGroupMutation.isPending}
              error={quickCreateError || undefined}
            />
          </SheetBody>
        </SheetContent>
      </Sheet>
    </div>
  );
}
