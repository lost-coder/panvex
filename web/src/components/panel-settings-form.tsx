import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { apiClient } from "../lib/api";
import { toggleAccordionSection } from "./settings-accordion-state";
import { AccordionSection, ErrorText, Field, SelectField, SettingsState } from "./settings-shared";

type PanelSettingsDraft = {
  http_public_url: string;
  grpc_public_endpoint: string;
};

const emptyDraft: PanelSettingsDraft = {
  http_public_url: "",
  grpc_public_endpoint: ""
};

export function PanelSettingsForm() {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<PanelSettingsDraft>(emptyDraft);
  const [expandedSection, setExpandedSection] = useState<string | null>(null);
  const [restartRequested, setRestartRequested] = useState(false);

  const settingsQuery = useQuery({
    queryKey: ["panel-settings"],
    queryFn: () => apiClient.panelSettings()
  });

  useEffect(() => {
    if (!settingsQuery.data) {
      return;
    }

    setDraft({
      http_public_url: settingsQuery.data.http_public_url,
      grpc_public_endpoint: settingsQuery.data.grpc_public_endpoint
    });
  }, [settingsQuery.data]);

  const saveMutation = useMutation({
    mutationFn: () => apiClient.updatePanelSettings(draft),
    onSuccess: async (response) => {
      setRestartRequested(false);
      queryClient.setQueryData(["panel-settings"], response);
      await queryClient.invalidateQueries({ queryKey: ["audit"] });
    }
  });
  const restartMutation = useMutation({
    mutationFn: () => apiClient.restartPanel(),
    onSuccess: async (response) => {
      setRestartRequested(true);
      queryClient.setQueryData(["panel-settings"], response);
      await queryClient.invalidateQueries({ queryKey: ["audit"] });
    }
  });

  if (settingsQuery.isLoading) {
    return <SettingsState title="Loading panel settings" description="Refreshing public endpoints, listeners, and TLS mode." />;
  }

  if (settingsQuery.isError || !settingsQuery.data) {
    return <SettingsState title="Panel settings are unavailable" description="The control-plane could not load the current panel configuration." />;
  }

  const current = restartMutation.data ?? saveMutation.data ?? settingsQuery.data;
  const errorMessage = restartMutation.error?.message ?? saveMutation.error?.message ?? null;
  const runtimeManagedByConfig = current.runtime_source === "config_file";
  const runtimeConfigPath = current.runtime_config_path || "config.toml";
  const bannerTone =
    restartRequested || runtimeManagedByConfig
      ? "border-amber-200 bg-amber-50 text-amber-900"
      : current.restart.state === "unavailable"
        ? "border-slate-200 bg-slate-100 text-slate-900"
        : "border-emerald-200 bg-emerald-50 text-emerald-900";

  return (
    <div className="space-y-6">
      <div className={`rounded-3xl border px-5 py-5 ${bannerTone}`}>
        <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.22em]">Restart</p>
            <h4 className="mt-2 text-lg font-semibold">
              {restartRequested
                ? "Restart requested"
                : runtimeManagedByConfig
                ? "Runtime settings are managed in the panel config file"
                : current.restart.state === "unavailable"
                  ? "Restart is unavailable in the current runtime mode"
                  : "Runtime follows the current process startup"}
            </h4>
            <p className="mt-2 text-sm leading-6 opacity-85">
              {restartRequested
                ? "Restart requested. If the panel runs under a supervisor, it should come back with the saved runtime settings shortly."
                : runtimeManagedByConfig
                  ? `Public endpoints still save here, but listeners, TLS, and the HTTP root path come from ${runtimeConfigPath}. Edit that file and restart the panel to change runtime behavior.`
                : current.restart.state === "unavailable"
                    ? "Public endpoints save here, but runtime values come from the current process startup. Restart the panel through its supervisor after changing startup parameters."
                    : "Public endpoints save here. Runtime values below are effective values from the running process and can be refreshed by restarting the panel after external config changes."}
            </p>
          </div>
          <button
            type="button"
            className="rounded-2xl border border-current/20 px-5 py-3 text-sm font-medium opacity-70"
            disabled={!current.restart.supported || restartMutation.isPending}
            title={current.restart.supported ? "Restart the supervised panel process." : "Restart is not available in the current runtime."}
            onClick={() => restartMutation.mutate()}
          >
            {restartMutation.isPending ? "Requesting restart..." : "Restart panel"}
          </button>
        </div>
      </div>

      <AccordionSection
        title="Public endpoints"
        description="Choose how browsers and agents should find the panel from the outside."
        open={expandedSection === "public"}
        onToggle={() => setExpandedSection((currentSection) => toggleAccordionSection(currentSection, "public"))}
      >
        <div className="grid gap-4 xl:grid-cols-2">
          <Field
            label="HTTP public URL"
            value={draft.http_public_url}
            onChange={(value) => setDraft((currentDraft) => ({ ...currentDraft, http_public_url: value }))}
            placeholder="https://panel.example.com"
            helperText="This is the browser-facing URL users open to reach the panel."
          />
          <Field
            label="gRPC public endpoint"
            value={draft.grpc_public_endpoint}
            onChange={(value) => setDraft((currentDraft) => ({ ...currentDraft, grpc_public_endpoint: value }))}
            placeholder="grpc.panel.example.com:443"
            helperText="Panvex shares this endpoint with agents after bootstrap."
          />
        </div>
      </AccordionSection>

      <AccordionSection
        title="Local listeners and TLS"
        description={
          runtimeManagedByConfig
            ? `These values are currently loaded from ${runtimeConfigPath} and are shown here for reference.`
            : "These are the effective runtime values of the current process. Change startup parameters or the panel config file to update them."
        }
        open={expandedSection === "runtime"}
        onToggle={() => setExpandedSection((currentSection) => toggleAccordionSection(currentSection, "runtime"))}
      >
        <div className="grid gap-4 xl:grid-cols-2">
          <Field
            label="HTTP root path"
            value={current.http_root_path}
            onChange={() => {}}
            placeholder="/panvex"
            helperText="If set, the panel serves its UI, API, and event stream under this prefix."
            disabled
          />
          <Field
            label="HTTP listen address"
            value={current.http_listen_address}
            onChange={() => {}}
            placeholder=":8080"
            disabled
          />
          <Field
            label="gRPC listen address"
            value={current.grpc_listen_address}
            onChange={() => {}}
            placeholder=":8443"
            disabled
          />
          <SelectField
            label="TLS mode"
            value={current.tls_mode}
            onChange={() => {}}
            options={[
              { value: "proxy", label: "Behind a reverse proxy" },
              { value: "direct", label: "Serve TLS directly" }
            ]}
            helperText="Choose direct TLS only when the panel itself should present the certificate."
            disabled
          />
        </div>

        {current.tls_mode === "direct" ? (
          <div className="mt-4 grid gap-4 xl:grid-cols-2">
            <Field
              label="Certificate file path"
              value={current.tls_cert_file}
              onChange={() => {}}
              placeholder="/etc/panvex-panel/tls/panel.crt"
              disabled
            />
            <Field
              label="Private key file path"
              value={current.tls_key_file}
              onChange={() => {}}
              placeholder="/etc/panvex-panel/tls/panel.key"
              disabled
            />
          </div>
        ) : null}
      </AccordionSection>

      {errorMessage ? <ErrorText message={errorMessage} /> : null}

      <div className="flex items-center justify-end">
        <button
          type="button"
          className="rounded-2xl bg-slate-950 px-5 py-3 text-sm font-medium text-white transition hover:bg-slate-800 disabled:opacity-60"
          onClick={() => saveMutation.mutate()}
          disabled={saveMutation.isPending}
        >
          {saveMutation.isPending ? "Saving..." : "Save changes"}
        </button>
      </div>
    </div>
  );
}
