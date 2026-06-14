import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { useNowSec } from "@/shared/hooks/useNowSec";
import { EnrollmentWizard } from "@/features/enrollment/EnrollmentWizard";
import type {
  EnrollmentWizardProps,
  EnrollmentMode,
  ScriptSourceKind,
} from "@/shared/api/types-pages/pages";
import { useFleetGroups } from "./hooks/useFleetGroups";
import { useFleetGroupMutations } from "@/features/fleet-groups/hooks/useFleetGroupsFull";
import { FleetGroupFormSheet, type FleetGroupFormData } from "@/features/fleet-groups/FleetGroupFormSheet";
import { EnrollmentLiveSection } from "./enrollment/EnrollmentLiveSection";
import { Sheet, SheetBody, SheetContent } from "@/ui";
import { useToast } from "@/app/providers/ToastProvider";
import { useConfirm } from "@/app/providers/ConfirmProvider";
import { useNavigate } from "@tanstack/react-router";
import { apiClient } from "@/shared/api/api";
import type {
  Agent,
  EnrollmentTokenResponse,
  ProvisionOutboundAgentResponse,
} from "@/shared/api/api";
import {
  DEFAULT_TELEMT_METRICS_URL,
  DEFAULT_TELEMT_URL,
} from "@/shared/lib/defaults";
import { isValidNodeName } from "@/shared/lib/shell-quote";
import { buildInstallCommand } from "./install-command";

const POLL_INTERVAL_MS = 3000;
const MAX_CONSECUTIVE_FAILURES = 3;
const FALLBACK_WINDOW_SECS = 300;

function terminalTokenErrorKey(status: string | undefined): string | null {
  if (status === "expired") return "error.tokenExpired";
  if (status === "revoked") return "error.tokenRevoked";
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
  const { t } = useTranslation("servers");
  const navigate = useNavigate();
  const toast = useToast();
  const confirm = useConfirm();
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

  // PR-3b: transport mode + outbound fields. Default to inbound so
  // operators landing on the wizard see the existing flow unchanged
  // — outbound is a deliberate radio click, not an accidental switch.
  const [mode, setMode] = useState<EnrollmentMode>("inbound");
  const [dialAddress, setDialAddress] = useState("");
  // The default source flips per mode: inbound users typically have a
  // reachable panel (Panel + SHA-256 self-check); outbound users have
  // a firewalled panel relative to the agent host, so GitHub is the
  // sensible default. Container nudges scriptSource whenever the
  // operator flips mode (below).
  const [scriptSource, setScriptSource] = useState<ScriptSourceKind>("panel");

  const [tokenData, setTokenData] = useState<EnrollmentTokenResponse | null>(null);
  // PR-3b: outbound branch stores the provision response so step 2
  // renders the pre-baked command verbatim and step 3 polls the
  // resulting agent_id. Mutually exclusive with tokenData.
  const [outboundData, setOutboundData] = useState<ProvisionOutboundAgentResponse | null>(null);
  // Expiry is stored as the absolute unix deadline; the rendered
  // "Expires in N min" is derived from useNowSec so it stays live
  // (30 s tick) instead of freezing at mint time (audit E1).
  const [tokenExpiresAtUnix, setTokenExpiresAtUnix] = useState<number | null>(null);

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

  const nowSec = useNowSec();
  const tokenExpiresInSecs =
    tokenExpiresAtUnix === null ? 0 : Math.max(0, tokenExpiresAtUnix - nowSec);

  // Bumping pollEpoch re-arms the step-3 polling effects after they
  // returned on MAX_CONSECUTIVE_FAILURES — the "Retry" action in
  // ConnectStep (audit E1: polling silently halted forever).
  const [pollEpoch, setPollEpoch] = useState(0);
  const handleRetryPolling = useCallback(() => {
    setError(undefined);
    setPollEpoch((e) => e + 1);
  }, []);

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

  // Switching mode resets scriptSource to the sensible default for
  // the new mode so an operator doesn't carry "Panel" into outbound
  // and end up with a curl the firewalled agent can't reach. The
  // operator can still re-pick the toggle afterwards.
  const handleModeChange = useCallback((next: EnrollmentMode) => {
    setMode(next);
    setScriptSource(next === "outbound" ? "github" : "panel");
  }, []);

  const panelUrl = tokenData?.panel_url ?? "";
  const tokenValue = tokenData?.value ?? "";

  // Resolve which install-script URL the inbound curl points at.
  // Prefer the backend's `script_sources` payload (PR-2a) when present
  // — that's the exact URL/digest the panel is serving. Fall back to
  // a derived `<panel>/install-agent.sh` for older backends so the
  // Panel toggle is never blocked by missing payload.
  const panelScriptUrl =
    tokenData?.script_sources?.panel.url ??
    (panelUrl ? `${panelUrl.replace(/\/+$/, "")}/install-agent.sh` : "");
  const githubScriptUrl =
    tokenData?.script_sources?.github.url ??
    "https://raw.githubusercontent.com/lost-coder/panvex/main/deploy/install-agent.sh";
  const inboundScriptUrl =
    scriptSource === "panel" && panelScriptUrl ? panelScriptUrl : githubScriptUrl;

  const inboundInstallCommand = tokenData
    ? buildInstallCommand(
        panelUrl,
        tokenValue,
        nodeName,
        advancedOptions,
        inboundScriptUrl,
      )
    : "";
  const outboundInstallCommand = outboundData?.command ?? "";
  const installCommand =
    mode === "outbound" ? outboundInstallCommand : inboundInstallCommand;

  // The shared "Token: …  Expires in: N min" footer renders either
  // the enrollment-token value (inbound) or a short agent_id ribbon
  // for outbound — both stay anchored at the bottom of step 2/3.
  const wizardTokenValue =
    mode === "outbound" ? (outboundData?.agent_id ?? "") : tokenValue;

  const handleGenerateToken = useCallback(async () => {
    // Validate before the network round-trip so the operator gets an
    // immediate, scoped error. The same predicate also keeps anything
    // shell-unsafe out of the rendered install command.
    if (!isValidNodeName(nodeName)) {
      setError(t("error.nodeName"));
      return;
    }
    setLoading(true);
    setError(undefined);
    try {
      if (mode === "outbound") {
        if (!dialAddress.trim()) {
          setError(t("error.dialAddressRequired"));
          return;
        }
        const result = await apiClient.provisionOutboundAgent({
          node_name: nodeName,
          fleet_group_id: selectedFleetGroup,
          dial_address: dialAddress.trim(),
          script_source: scriptSource,
          advanced: {
            telemt_url:
              advancedOptions.telemtUrl !== DEFAULT_TELEMT_URL
                ? advancedOptions.telemtUrl
                : null,
            telemt_metrics_url:
              advancedOptions.telemtMetricsUrl !== DEFAULT_TELEMT_METRICS_URL
                ? advancedOptions.telemtMetricsUrl
                : null,
            telemt_auth: advancedOptions.telemtAuth || null,
            insecure_transport: advancedOptions.insecureTransport || null,
          },
        });
        setOutboundData(result);
        setTokenData(null);
        setTokenExpiresAtUnix(result.expires_at_unix);
        setStep(2);
        return;
      }
      const result = await apiClient.createEnrollmentToken({
        fleet_group_id: selectedFleetGroup,
        ttl_seconds: tokenTtl,
      });
      setTokenData(result);
      setOutboundData(null);
      setTokenExpiresAtUnix(result.expires_at_unix);
      setStep(2);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error.tokenCreateFailed"));
    } finally {
      setLoading(false);
    }
  }, [
    selectedFleetGroup,
    tokenTtl,
    nodeName,
    mode,
    dialAddress,
    scriptSource,
    advancedOptions,
    t,
  ]);

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

  // Outbound cancel/close path needs to clean up the agent row we
  // pre-provisioned. Best-effort: if the DELETE fails the row will be
  // pruned by the panel's sweep once the bootstrap token expires.
  const cleanupOutbound = useCallback(async () => {
    if (mode !== "outbound" || !outboundData?.agent_id) return;
    try {
      await apiClient.deregisterAgent(outboundData.agent_id);
    } catch (err) {
      // Don't block close on cleanup failures — show a toast so the
      // operator knows to verify the agent list, but do not throw.
      toast.error(
        err instanceof Error
          ? t("error.cleanupFailed", { message: err.message })
          : t("error.cleanupFallback"),
      );
    }
  }, [mode, outboundData, toast, t]);

  // Closing mid-flow discards wizard progress and the minted token, so
  // steps 2/3 are gated behind a confirm (audit E1). Step 1 has nothing
  // to lose — close immediately.
  const attemptClose = useCallback(async () => {
    if (step > 1) {
      const ok = await confirm({
        title: t("addServer.closeConfirmTitle"),
        body: t("addServer.closeConfirmBody"),
        confirmLabel: t("addServer.closeConfirmAction"),
        cancelLabel: t("addServer.closeConfirmCancel"),
        variant: "danger",
      });
      if (!ok) return;
    }
    void cleanupOutbound();
    void navigate({ to: "/servers" });
  }, [step, confirm, cleanupOutbound, navigate, t]);

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

  // Inbound polling: watches enrollment-tokens + agents for the
  // freshly-bootstrapped agent. Unchanged from the pre-PR-3b flow.
  useEffect(() => {
    if (step !== 3 || mode !== "inbound" || !tokenValue) return;

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
      const ourToken = tokens.find((tk) => tk.value === tokenValue);
      const terminalKey = terminalTokenErrorKey(ourToken?.status);
      if (terminalKey) {
        setError(t(terminalKey));
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
            setError(t("error.probeFailed", { count: consecutiveFailures, reason }));
            return;
          }
        }
      }
    };
    void poll();
    return () => {
      cancelled = true;
    };
  }, [step, mode, tokenValue, nodeName, applyAgentStatus, t, pollEpoch]);

  // Outbound polling: the agent_id is already known (we created it).
  // We just wait for the panel's outbound supervisor to land a session
  // — same three stages but bootstrap is "done" as soon as the agent
  // record exists. Gateway/firstData fall out of `presence_state` and
  // `runtime` the same way as inbound.
  useEffect(() => {
    if (step !== 3 || mode !== "outbound" || !outboundData?.agent_id) return;

    const targetId = outboundData.agent_id;
    let cancelled = false;
    let consecutiveFailures = 0;

    setConnectionStatus((prev) =>
      prev.bootstrap === "done" ? prev : { ...prev, bootstrap: "done" },
    );

    const probeOnce = async (): Promise<boolean> => {
      const agents: Agent[] = await apiClient.agents();
      const match = agents.find((a) => a.id === targetId);
      if (match) return applyAgentStatus(match);
      // Row vanished mid-poll — only happens if the operator (or another
      // admin) deregistered the agent while the wizard was watching.
      // Surface as an error so the operator can restart.
      setError(t("error.outboundMissing"));
      return true;
    };

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
            setError(t("error.probeFailed", { count: consecutiveFailures, reason }));
            return;
          }
        }
      }
    };
    void poll();
    return () => {
      cancelled = true;
    };
  }, [step, mode, outboundData, applyAgentStatus, t, pollEpoch]);

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
      toast.success(t("addServer.fleetGroupCreated", { label: created.label }));
      setSelectedFleetGroup(created.id);
      setQuickCreateOpen(false);
      setQuickCreateData({ name: "", label: "", description: "" });
    } catch (err) {
      setQuickCreateError(err instanceof Error ? err.message : t("error.requestFailed"));
    }
  };

  return (
    <>
      <Sheet
        open
        onOpenChange={(open) => {
          // Radix requests close on Escape/overlay click; with a controlled
          // `open` we route the request through the step-aware confirm.
          if (!open) void attemptClose();
        }}
      >
        <SheetContent side="center" title={t("addServer.ariaLabel")}>
          <div className="relative p-6">
            <button
              type="button"
              onClick={() => void attemptClose()}
              className="absolute top-3 right-3 p-2 rounded-xs text-fg-muted hover:text-fg hover:bg-bg-hover transition-colors"
              aria-label={t("addServer.close")}
            >
              <X size={18} aria-hidden="true" />
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
              mode={mode}
              onModeChange={handleModeChange}
              dialAddress={dialAddress}
              onDialAddressChange={setDialAddress}
              scriptSource={scriptSource}
              onScriptSourceChange={setScriptSource}
              installCommand={installCommand}
              tokenValue={wizardTokenValue}
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
              onCancel={() => void attemptClose()}
              onRetryPolling={handleRetryPolling}
              onViewAttempts={() => void navigate({ to: "/enrollment-attempts" })}
              loading={loading}
              error={error}
            />
            {/*
              Phase-1 observability: once the bootstrap probe resolves an
              agent ID, surface the enrollment-attempts timeline so operators
              can see exactly which step the agent reached (or which one
              failed). Hidden until the agent is known — the wizard's
              built-in three-stage status covers the pre-bootstrap window.
              For outbound we already have the id from provision response,
              so the timeline lights up immediately.
            */}
            <EnrollmentLiveSection
              agentId={connectedAgent?.id ?? outboundData?.agent_id ?? null}
            />
          </div>
        </SheetContent>
      </Sheet>

      {/* quick-create sheet stays as a sibling portal */}
      <Sheet
        open={quickCreateOpen}
        onOpenChange={(open) => { if (!open) setQuickCreateOpen(false); }}
      >
        <SheetContent
          side="bottom"
          title={t("addServer.quickCreateTitle")}
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
    </>
  );
}
