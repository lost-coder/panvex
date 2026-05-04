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
import { Globe } from "lucide-react";
import { Button, Input, PageSection, SettingsRow } from "@/ui";
import { useGeoIPSettings } from "./hooks/useGeoIPSettings";
import type {
  GeoIPResponseParsed,
  GeoIPSettingsParsed,
} from "@/shared/api/schemas";

type UseGeoIPSettings = ReturnType<typeof useGeoIPSettings>;

type Mode = GeoIPSettingsParsed["mode"];
type SourceState = GeoIPResponseParsed["state"]["city"];

const MODES: ReadonlyArray<{ value: Mode; label: string; help: string }> = [
  { value: "", label: "Disabled", help: "No GeoIP enrichment." },
  {
    value: "auto",
    label: "Auto (P3TERX)",
    help: "Refreshed weekly from github.com/P3TERX/GeoLite.mmdb.",
  },
  {
    value: "url",
    label: "Custom URLs",
    help: "Periodic download from operator-supplied URLs.",
  },
  {
    value: "local",
    label: "Local files",
    help: "Operator-managed files on disk.",
  },
];

function formatTimestamp(unix: number): string {
  if (!unix) return "never";
  return new Date(unix * 1000).toUTCString();
}

function formatBytes(n: number): string {
  if (!n) return "—";
  if (n > 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  if (n > 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${n} B`;
}

export function GeoIPSettingsSection() {
  const hook = useGeoIPSettings();
  const { response, isLoading } = hook;

  if (isLoading || !response) {
    return (
      <PageSection
        icon={Globe}
        title="GeoIP"
        description="Country / city / ASN enrichment for the IP history table."
      >
        <div className="px-4 py-3 text-sm text-fg-muted">Loading…</div>
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
  const refreshSupported = draft.mode !== "";

  return (
    <PageSection
      icon={Globe}
      title="GeoIP"
      description="Country / city / ASN enrichment for the IP history table."
    >
      <SettingsRow label="Mode">
        <div role="radiogroup" aria-label="GeoIP mode" className="flex flex-col gap-1">
          {MODES.map((m) => {
            const id = `geoip-mode-${m.value || "disabled"}`;
            return (
              <div key={m.value || "disabled"} className="flex items-start gap-2 text-sm">
                <input
                  id={id}
                  type="radio"
                  name="geoip-mode"
                  className="mt-0.5 h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
                  checked={draft.mode === m.value}
                  onChange={() => setMode(m.value)}
                />
                <label htmlFor={id} className="cursor-pointer">
                  <span className="font-medium text-fg">{m.label}</span>
                  <span className="block text-xs text-fg-muted">{m.help}</span>
                </label>
              </div>
            );
          })}
        </div>
      </SettingsRow>

      {draft.mode === "url" && (
        <>
          <SettingsRow label="City URL">
            <Input
              className="w-80"
              value={draft.city.url}
              placeholder="https://…/GeoLite2-City.mmdb"
              onChange={(e) => patchSrc("city", { url: e.target.value })}
            />
          </SettingsRow>
          <SettingsRow label="ASN URL">
            <Input
              className="w-80"
              value={draft.asn.url}
              placeholder="https://…/GeoLite2-ASN.mmdb"
              onChange={(e) => patchSrc("asn", { url: e.target.value })}
            />
          </SettingsRow>
        </>
      )}

      {draft.mode === "local" && (
        <>
          <SettingsRow label="City file path">
            <Input
              className="w-80"
              value={draft.city.local_path}
              placeholder="/var/lib/panvex/geoip/city.mmdb"
              onChange={(e) => patchSrc("city", { local_path: e.target.value })}
            />
          </SettingsRow>
          <SettingsRow label="ASN file path">
            <Input
              className="w-80"
              value={draft.asn.local_path}
              placeholder="/var/lib/panvex/geoip/asn.mmdb"
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
          {save.isPending ? "Saving…" : "Save"}
        </Button>
        {refreshSupported && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => refresh.mutate()}
            disabled={refresh.isPending}
          >
            {refresh.isPending ? "Updating…" : "Update now"}
          </Button>
        )}
      </div>

      <SettingsRow label="Status">
        <StatusBlock state={initial} />
      </SettingsRow>
    </PageSection>
  );
}

function StatusBlock({ state }: Readonly<{ state: GeoIPResponseParsed }>) {
  return (
    <div className="text-xs font-mono space-y-1 text-right">
      <StatusRow label="City" s={state.state.city} />
      <StatusRow label="ASN" s={state.state.asn} />
    </div>
  );
}

function StatusRow({ label, s }: Readonly<{ label: string; s: SourceState }>) {
  return (
    <div>
      <span className="text-fg-muted">{label}: </span>
      <span className="text-fg">
        {formatTimestamp(s.last_updated_at)} · {formatBytes(s.size_bytes)}
      </span>
      {s.error ? (
        <div className="text-status-error">error: {s.error}</div>
      ) : null}
    </div>
  );
}
