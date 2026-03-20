import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { apiClient } from "../lib/api";
import {
  type AppearanceDensity,
  type AppearanceDraft,
  type AppearanceTheme,
  buildAppearanceDraft,
  getAppearanceQueryKey,
  syncAppearanceDraft
} from "../lib/appearance";
import { ErrorText, SettingsState } from "./settings-shared";

export function AppearanceSettingsForm(props: { userID: string }) {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<AppearanceDraft>(buildAppearanceDraft(undefined));
  const [isDirty, setIsDirty] = useState(false);
  const appearanceQueryKey = getAppearanceQueryKey(props.userID);

  const appearanceQuery = useQuery({
    queryKey: appearanceQueryKey,
    queryFn: () => apiClient.appearanceSettings(),
    retry: false
  });

  useEffect(() => {
    setDraft((currentDraft) => syncAppearanceDraft(currentDraft, appearanceQuery.data, isDirty));
  }, [appearanceQuery.data, isDirty]);

  const saveMutation = useMutation({
    mutationFn: () => apiClient.updateAppearanceSettings(draft),
    onSuccess: async (response) => {
      setDraft(buildAppearanceDraft(response));
      setIsDirty(false);
      queryClient.setQueryData(appearanceQueryKey, response);
      await queryClient.invalidateQueries({ queryKey: appearanceQueryKey });
    }
  });

  if (appearanceQuery.isLoading && !appearanceQuery.data) {
    return <SettingsState title="Loading appearance" description="Refreshing your current theme and density preferences." />;
  }

  if (appearanceQuery.isError && !appearanceQuery.data) {
    return <SettingsState title="Appearance is unavailable" description="The control-plane could not load your current interface preferences." />;
  }

  const errorMessage = saveMutation.error?.message ?? null;

  return (
    <div className="space-y-6">
      <ChoiceGroup
        label="Theme"
        description="Choose whether the interface should stay bright, turn dark, or follow the operating system."
        value={draft.theme}
        options={[
          { value: "system", label: "System" },
          { value: "light", label: "Light" },
          { value: "dark", label: "Dark" }
        ]}
        onChange={(value) => {
          setIsDirty(true);
          setDraft((currentDraft) => ({ ...currentDraft, theme: value as AppearanceTheme }));
        }}
      />

      <ChoiceGroup
        label="Density"
        description="Keep the current spacious rhythm or tighten cards and controls for denser operator views."
        value={draft.density}
        options={[
          { value: "comfortable", label: "Comfortable" },
          { value: "compact", label: "Compact" }
        ]}
        onChange={(value) => {
          setIsDirty(true);
          setDraft((currentDraft) => ({ ...currentDraft, density: value as AppearanceDensity }));
        }}
      />

      {errorMessage ? <ErrorText message={errorMessage} /> : null}

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          className="app-button-primary rounded-2xl"
          onClick={() => saveMutation.mutate()}
          disabled={saveMutation.isPending}
        >
          {saveMutation.isPending ? "Saving..." : "Save appearance"}
        </button>
        <p className="text-sm text-[var(--app-text-secondary)]">
          These preferences stay attached to your account across sessions and browsers.
        </p>
      </div>
    </div>
  );
}

function ChoiceGroup(props: {
  label: string;
  description: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
}) {
  return (
    <div>
      <div>
        <p className="text-sm font-medium text-[var(--app-text-primary)]">{props.label}</p>
        <p className="mt-2 text-sm leading-6 text-[var(--app-text-secondary)]">{props.description}</p>
      </div>
      <div className="mt-4 flex flex-wrap gap-3">
        {props.options.map((option) => {
          const active = props.value === option.value;
          return (
            <button
              key={option.value}
              type="button"
              className={`rounded-2xl px-4 py-3 text-sm font-medium transition ${
                active ? "app-tab-active" : "app-tab-inactive"
              }`}
              onClick={() => props.onChange(option.value)}
            >
              {option.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
