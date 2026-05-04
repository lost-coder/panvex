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

const POLL_INTERVAL_MS = 3000;
const MAX_CONSECUTIVE_FAILURES = 3;
const FALLBACK_WINDOW_SECS = 300;

function terminalTokenError(status: string | undefined): string | null {
  if (status === "expired") {
    return "Enrollment token expired before the agent dialed in. Generate a new token and re-run the install command.";
  }
  if (status === "revoked") {
    return "Enrollment token was revoked. Generate a new token and re-run the install command.";
  }
  return null;
}

// Match a freshly-bootstrapped agent by cert_issued_at within a window
// around the token's consumed_at. Picks the most-recent cert-issue on
// tie so repeated bootstraps still resolve to the latest agent.
function findFallbackAgent(
  agents: Agent[],
  consumedAt: number,
): Agent | null {
  const candidates = agents
    .filter((a) => a.cert_issued_at)
    .map((a) => {
      const t = Date.parse(a.cert_issued_at!);
      return Number.isFinite(t) ? { a, issuedAt: Math.floor(t / 1000) } : null;
    })
    .filter(
      (x): x is { a: Agent; issuedAt: number } =>
        x !== null && Math.abs(x.issuedAt - consumedAt) < FALLBACK_WINDOW_SECS,
    )
    .sort((x, y) => y.issuedAt - x.issuedAt);
  return candidates[0]?.a ?? null;
}

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

  // Apply the three-stage progression once we've picked an agent to follow.
  // Returns true when all three stages landed (caller should stop polling).
  const applyAgentStatus = useCallback((match: Agent): boolean => {
    const online = match.presence_state === "online";
    const hasRuntime = Boolean(match.runtime);
    if (online && hasRuntime) {
      setConnectionStatus({ bootstrap: "done", grpcConnect: "done", firstData: "done" });
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
  }, []);

  useEffect(() => {
    if (step !== 3 || !tokenValue) return;

    let cancelled = false;
    let consecutiveFailures = 0;

    // Three-stage progression:
    //   1. Bootstrap  → token consumed in the backend
    //   2. Gateway    → agent record appears (presence != offline)
    //   3. First data → presence_state === "online" with runtime telemetry
    // Returns true when polling should stop.
    const probeOnce = async (): Promise<boolean> => {
      const agents: Agent[] = await apiClient.agents();
      const match = agents.find((a) => a.node_name === nodeName);
      if (match) return applyAgentStatus(match);

      const tokens = await apiClient.listEnrollmentTokens();
      const ourToken = tokens.find((t) => t.value === tokenValue);
      const terminal = terminalTokenError(ourToken?.status);
      if (terminal) {
        setError(terminal);
        return true;
      }
      if (ourToken?.status === "consumed" && ourToken.consumed_at_unix) {
        // Fallback: the agent didn't register under the node_name the
        // operator typed (install script fell back to hostname, etc.).
        const candidate = findFallbackAgent(agents, ourToken.consumed_at_unix);
        if (candidate) return applyAgentStatus(candidate);
        // Token consumed, no agent match yet → bootstrap done, gateway waiting.
        setConnectionStatus({ bootstrap: "done", grpcConnect: "waiting", firstData: "pending" });
      }
      return false;
    };

    // After MAX_CONSECUTIVE_FAILURES probes in a row we surface the backend
    // error instead of silently polling forever. Transient glitches reset
    // the counter as soon as a probe succeeds.
    const poll = async () => {
      while (!cancelled) {
        await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
        if (cancelled) break;
        try {
          if (await probeOnce()) return;
          consecutiveFailures = 0;
        } catch (err) {
          consecutiveFailures++;
          if (consecutiveFailures >= MAX_CONSECUTIVE_FAILURES) {
            const reason = err instanceof Error ? `: ${err.message}` : ".";
            setError(`Probe failed ${consecutiveFailures}× in a row${reason}`);
            return;
          }
        }
      }
    };
    void poll();
    return () => {
      cancelled = true;
    };
  }, [step, tokenValue, nodeName, applyAgentStatus]);

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
      <section
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
              void navigate({
                to: "/servers/$serverId",
                params: { serverId: connectedAgent.id },
              });
            }
          }}
          onCancel={goBack}
          loading={loading}
          error={error}
        />
      </section>

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
