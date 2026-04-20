import { MonoValue } from "@/ui/primitives";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

/**
 * System-info view. Earlier revisions bundled selftest + meQuality +
 * natStun + networkPath into this tab, but per review the only items
 * operators actually use on the panel are the Telemt version and the
 * config reload count. The heavier diagnostics remain accessible via
 * the raw telemt API; this tab keeps the overview tidy.
 */
export function DiagnosticsTab({ server }: { server: ServerDetailPageProps["server"] }) {
  const { systemInfo } = server;
  return (
    <div className="flex flex-col gap-2 text-sm">
      <Row label="Telemt version" value={<MonoValue>{systemInfo.version}</MonoValue>} />
      <Row
        label="Config reloads"
        value={<MonoValue>{systemInfo.configReloadCount.toLocaleString()}</MonoValue>}
      />
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 py-2 border-b border-dashed border-divider last:border-b-0">
      <span className="text-fg-muted">{label}</span>
      {value}
    </div>
  );
}
