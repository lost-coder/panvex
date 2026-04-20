import { useState, useEffect, useCallback } from "react";
import { EnrollmentWizard } from "@/features/enrollment/EnrollmentWizard";
import type { EnrollmentWizardProps } from "@/shared/api/types-pages/pages";
import { useFleetGroups } from "./hooks/useFleetGroups";
import { useNavigate } from "@tanstack/react-router";
import { apiClient } from "@/shared/api/api";
import type { EnrollmentTokenResponse, Agent } from "@/shared/api/api";

const GITHUB_REPO = "lost-coder/panvex";

function buildInstallCommand(
  panelUrl: string,
  tokenValue: string,
  nodeName: string,
  advancedOptions?: { telemtUrl: string; telemtMetricsUrl: string; telemtAuth: string },
) {
  let cmd =
    `curl -fsSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy/install-agent.sh | \\\n` +
    `  sudo bash -s -- \\\n` +
    `    --panel-url ${panelUrl} \\\n` +
    `    --token ${tokenValue} \\\n` +
    `    --node-name ${nodeName}`;

  if (advancedOptions?.telemtUrl && advancedOptions.telemtUrl !== "http://127.0.0.1:9091") {
    cmd += ` \\\n    --telemt-url ${advancedOptions.telemtUrl}`;
  }
  // Metrics URL is a first-class knob in the wizard because Telemt
  // ships with metrics off. Only append the flag when the operator
  // changed it from the agent's built-in default.
  if (
    advancedOptions?.telemtMetricsUrl &&
    advancedOptions.telemtMetricsUrl !== "http://127.0.0.1:8081"
  ) {
    cmd += ` \\\n    --telemt-metrics-url ${advancedOptions.telemtMetricsUrl}`;
  }
  if (advancedOptions?.telemtAuth) {
    cmd += ` \\\n    --telemt-auth ${advancedOptions.telemtAuth}`;
  }
  return cmd;
}

export function AddServerContainer() {
  const navigate = useNavigate();
  const { fleetGroups } = useFleetGroups();

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [nodeName, setNodeName] = useState("");
  const [selectedFleetGroup, setSelectedFleetGroup] = useState("");
  const [tokenTtl, setTokenTtl] = useState(3600);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [advancedOptions, setAdvancedOptions] = useState({
    telemtUrl: "http://127.0.0.1:9091",
    telemtMetricsUrl: "http://127.0.0.1:8081",
    telemtAuth: "",
  });

  const [tokenData, setTokenData] = useState<EnrollmentTokenResponse | null>(null);

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
      setSelectedFleetGroup(first.id);
    }
  }, [fleetGroups, selectedFleetGroup]);

  const panelUrl = tokenData?.panel_url ?? "";
  const tokenValue = tokenData?.value ?? "";
  const tokenExpiresInSecs = tokenData
    ? Math.max(0, tokenData.expires_at_unix - Math.floor(Date.now() / 1000))
    : 0;

  const installCommand = tokenData
    ? buildInstallCommand(panelUrl, tokenValue, nodeName, advancedOptions)
    : "";

  const handleGenerateToken = useCallback(async () => {
    setLoading(true);
    setError(undefined);
    try {
      const result = await apiClient.createEnrollmentToken({
        fleet_group_id: selectedFleetGroup,
        ttl_seconds: tokenTtl,
      });
      setTokenData(result);
      setStep(2);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create token");
    } finally {
      setLoading(false);
    }
  }, [selectedFleetGroup, tokenTtl]);

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
              return;
            }
            setConnectionStatus({
              bootstrap: "done",
              grpcConnect: online ? "done" : "waiting",
              firstData: online ? "waiting" : "pending",
            });
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
            if (ourToken?.status === "consumed") {
              setConnectionStatus({
                bootstrap: "done",
                grpcConnect: "waiting",
                firstData: "pending",
              });
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
    name: g.id,
    nodeCount: g.agent_count,
  }));

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div
        className="relative bg-bg-card rounded-lg border border-border shadow-xl w-full max-w-[480px] max-h-[85vh] overflow-y-auto mx-4 p-6"
        onClick={(e) => e.stopPropagation()}
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
    </div>
  );
}
