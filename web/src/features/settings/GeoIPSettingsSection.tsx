// Task 20 — admin-only GeoIP settings section.
//
// Lets the operator pick between Disabled / Auto (P3TERX) / Custom URLs /
// Local files for the City + ASN MaxMind databases that enrich the IP
// history table. URL/Local fields toggle visibility based on mode.
//
// Server contract (see shared/api/schemas/settings.ts):
//   - mode: "" | "auto" | "url" | "local"
//   - city / asn: { enabled, url, local_path }
//   - response.state.{city,asn}: timestamps + size + error from the loader.

import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { Globe } from "lucide-react";
import { Button, Input, PageSection, SettingsRow, formatBytes } from "@/ui";
import { useGeoIPSettings } from "./hooks/useGeoIPSettings";
import { useUnsavedChangesGuard } from "@/shared/hooks";
import type {
  GeoIPResponseParsed,
  GeoIPSettingsParsed,
} from "@/shared/api/schemas";

type UseGeoIPSettings = ReturnType<typeof useGeoIPSettings>;

type Mode = GeoIPSettingsParsed["mode"];
type SourceState = GeoIPResponseParsed["state"]["city"];

type ModeKey = "disabled" | "auto" | "url" | "local";

const MODE_DEFS: ReadonlyArray<{ value: Mode; key: ModeKey }> = [
  { value: "", key: "disabled" },
  { value: "auto", key: "auto" },
  { value: "url", key: "url" },
  { value: "local", key: "local" },
];

function formatTimestamp(unix: number, neverLabel: string): string {
  if (!unix) return neverLabel;
  return new Date(unix * 1000).toUTCString();
}

export function GeoIPSettingsSection() {
  const { t } = useTranslation("settings");
  const hook = useGeoIPSettings();
  const { response, isLoading } = hook;

  if (isLoading || !response) {
    return (
      <PageSection
        icon={Globe}
        title={t("geoip.title")}
        description={t("geoip.description")}
      >
        <div className="px-4 py-3 text-sm text-fg-muted">{t("geoip.loading")}</div>
      </PageSection>
    );
  }

  // The inner component is keyed on the server-side updated_at proxy
  // (concatenation of city + asn timestamps) so a fresh server response
  // resets the draft via remount instead of a setState-in-effect.
  const remountKey =
    `${response.state.city.last_updated_at}-${response.state.asn.last_updated_at}`;

  return <GeoIPForm key={remountKey} initial={response} hook={hook} />;
}

function GeoIPForm({
  initial,
  hook,
}: Readonly<{ initial: GeoIPResponseParsed; hook: UseGeoIPSettings }>) {
  const { t } = useTranslation("settings");
  const { save, refresh } = hook;
  const [draft, setDraft] = useState<GeoIPSettingsParsed>(initial.settings);

  function setMode(m: Mode) {
    setDraft((d) => {
      // The backend rejects any non-disabled mode where both sources are
      // disabled. A fresh panel arrives with both enabled=false (zero
      // value), and the UI doesn't expose per-source toggles, so picking
      // a mode here implicitly enables both. Operators with custom
      // per-source preferences will get UI toggles in a later iteration.
      if (m !== "" && !d.city.enabled && !d.asn.enabled) {
        return {
          ...d,
          mode: m,
          city: { ...d.city, enabled: true },
          asn: { ...d.asn, enabled: true },
        };
      }
      return { ...d, mode: m };
    });
  }

  function patchSrc(
    kind: "city" | "asn",
    patch: Partial<GeoIPSettingsParsed["city"]>,
  ) {
    setDraft((d) => ({ ...d, [kind]: { ...d[kind], ...patch } }));
  }

  // Avoid pointless round-trips when the form is unchanged. Stringify is
  // adequate here — the object is shallow and ~6 fields.
  const isDirty = JSON.stringify(draft) !== JSON.stringify(initial.settings);

  // Audit E4: guard in-app navigation while there are unsaved GeoIP changes.
  useUnsavedChangesGuard(isDirty);

  const refreshSupported = draft.mode !== "";

  return (
    <PageSection
      icon={Globe}
      title={t("geoip.title")}
      description={t("geoip.description")}
    >
      <SettingsRow label={t("geoip.modeLabel")}>
        <div role="radiogroup" aria-label={t("geoip.modeAriaLabel")} className="flex flex-col gap-1">
          {MODE_DEFS.map((m) => {
            const id = `geoip-mode-${m.key}`;
            return (
              <div key={m.key} className="flex items-start gap-2 text-sm">
                <input
                  id={id}
                  type="radio"
                  name="geoip-mode"
                  className="mt-0.5 h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
                  checked={draft.mode === m.value}
                  onChange={() => setMode(m.value)}
                />
                <label htmlFor={id} className="cursor-pointer">
                  <span className="font-medium text-fg">{t(`geoip.modes.${m.key}.label`)}</span>
                  <span className="block text-xs text-fg-muted">{t(`geoip.modes.${m.key}.help`)}</span>
                </label>
              </div>
            );
          })}
        </div>
      </SettingsRow>

      {draft.mode === "url" && (
        <>
          <SettingsRow label={t("geoip.cityUrlLabel")}>
            <Input
              className="w-80"
              value={draft.city.url}
              placeholder={t("geoip.cityUrlPlaceholder")}
              onChange={(e) => patchSrc("city", { url: e.target.value })}
            />
          </SettingsRow>
          <SettingsRow label={t("geoip.asnUrlLabel")}>
            <Input
              className="w-80"
              value={draft.asn.url}
              placeholder={t("geoip.asnUrlPlaceholder")}
              onChange={(e) => patchSrc("asn", { url: e.target.value })}
            />
          </SettingsRow>
        </>
      )}

      {draft.mode === "local" && (
        <>
          <SettingsRow label={t("geoip.cityPathLabel")}>
            <Input
              className="w-80"
              value={draft.city.local_path}
              placeholder={t("geoip.cityPathPlaceholder")}
              onChange={(e) => patchSrc("city", { local_path: e.target.value })}
            />
          </SettingsRow>
          <SettingsRow label={t("geoip.asnPathLabel")}>
            <Input
              className="w-80"
              value={draft.asn.local_path}
              placeholder={t("geoip.asnPathPlaceholder")}
              onChange={(e) => patchSrc("asn", { local_path: e.target.value })}
            />
          </SettingsRow>
        </>
      )}

      <div className="flex items-center justify-between gap-2 px-4 py-3">
        <Button
          size="sm"
          onClick={() => save.mutate(draft)}
          disabled={save.isPending || !isDirty}
        >
          {save.isPending ? t("geoip.saving") : t("geoip.save")}
        </Button>
        {refreshSupported && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => refresh.mutate()}
            disabled={refresh.isPending}
          >
            {refresh.isPending ? t("geoip.updatingNow") : t("geoip.updateNow")}
          </Button>
        )}
      </div>

      <SettingsRow label={t("geoip.statusLabel")}>
        <StatusBlock state={initial} t={t} />
      </SettingsRow>
    </PageSection>
  );
}

function StatusBlock({ state, t }: Readonly<{ state: GeoIPResponseParsed; t: TFunction }>) {
  return (
    <div className="text-xs font-mono space-y-1 text-right">
      <StatusRow label={t("geoip.statusCity")} s={state.state.city} t={t} />
      <StatusRow label={t("geoip.statusAsn")} s={state.state.asn} t={t} />
    </div>
  );
}

function StatusRow({ label, s, t }: Readonly<{ label: string; s: SourceState; t: TFunction }>) {
  const never = t("geoip.never");
  return (
    <div>
      <span className="text-fg-muted">{label}: </span>
      <span className="text-fg">
        {formatTimestamp(s.last_updated_at, never)} ·{" "}
        {s.size_bytes ? formatBytes(s.size_bytes) : "—"}
      </span>
      {s.error ? (
        <div className="text-status-error">{t("geoip.errorPrefix")}: {s.error}</div>
      ) : null}
    </div>
  );
}
