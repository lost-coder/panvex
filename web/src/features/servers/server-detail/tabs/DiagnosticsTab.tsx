import { FieldLabel, MonoValue } from "@/ui/primitives";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Badge } from "@/ui/primitives/Badge";
import { formatTime } from "@/ui/lib/format";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

export function DiagnosticsTab({ server }: { server: ServerDetailPageProps["server"] }) {
  const { selftest, meQuality, natStun, systemInfo, networkPath } = server;

  const stateIcon = (state: string) => {
    if (state === "ok" || state === "good" || state === "one") return "✓";
    return "✗";
  };
  const stateVariant = (state: string): "ok" | "error" => {
    if (state === "ok" || state === "good" || state === "one") return "ok";
    return "error";
  };

  return (
    <div className="flex flex-col gap-6 pt-2">
      {/* Self-test checklist */}
      {selftest?.enabled && (
        <div className="rounded-xs bg-bg-card p-4 flex flex-col gap-3">
          <FieldLabel>Self-Test</FieldLabel>
          <div className="flex flex-col gap-2 text-sm">
            <div className="flex items-center justify-between">
              <span className="text-fg-muted">KDF</span>
              <div className="flex items-center gap-2">
                <Badge variant={stateVariant(selftest.kdf.state)}>
                  {stateIcon(selftest.kdf.state)} {selftest.kdf.state}
                </Badge>
                <span className="text-xs text-fg-muted font-mono">
                  {selftest.kdf.ewmaErrorsPerMin.toFixed(1)} err/min
                </span>
              </div>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-fg-muted">Time Skew</span>
              <div className="flex items-center gap-2 flex-wrap justify-end">
                <Badge variant={stateVariant(selftest.timeskew.state)}>
                  {stateIcon(selftest.timeskew.state)} {selftest.timeskew.state}
                </Badge>
                {selftest.timeskew.maxSkewSecs15m != null && (
                  <span className="text-xs text-fg-muted font-mono">
                    max {selftest.timeskew.maxSkewSecs15m}s in 15m
                  </span>
                )}
                {selftest.timeskew.samples15m != null && (
                  <span className="text-xs text-fg-muted font-mono">
                    samples: {selftest.timeskew.samples15m}
                  </span>
                )}
                {selftest.timeskew.lastSkewSecs != null && (
                  <span className="text-xs text-fg-muted font-mono">
                    last: {selftest.timeskew.lastSkewSecs}s
                  </span>
                )}
                {selftest.timeskew.lastSource && (
                  <span className="text-xs text-fg-muted font-mono">
                    src: {selftest.timeskew.lastSource}
                  </span>
                )}
              </div>
            </div>
            {selftest.ip.v4 && (
              <div className="flex items-center justify-between">
                <span className="text-fg-muted">IP v4</span>
                <div className="flex items-center gap-2">
                  <Badge variant={stateVariant(selftest.ip.v4.state)}>
                    {stateIcon(selftest.ip.v4.state)} {selftest.ip.v4.state}
                  </Badge>
                  <span className="text-xs font-mono text-fg">{selftest.ip.v4.addr}</span>
                </div>
              </div>
            )}
            {selftest.ip.v6 && (
              <div className="flex items-center justify-between">
                <span className="text-fg-muted">IP v6</span>
                <div className="flex items-center gap-2">
                  <Badge variant={stateVariant(selftest.ip.v6.state)}>
                    {stateIcon(selftest.ip.v6.state)} {selftest.ip.v6.state}
                  </Badge>
                  <span className="text-xs font-mono text-fg">{selftest.ip.v6.addr}</span>
                </div>
              </div>
            )}
            <div className="flex items-center justify-between">
              <span className="text-fg-muted">PID</span>
              <div className="flex items-center gap-2">
                <Badge variant={stateVariant(selftest.pid.state)}>
                  {stateIcon(selftest.pid.state)} {selftest.pid.state}
                </Badge>
                <span className="text-xs font-mono text-fg-muted">PID: {selftest.pid.pid}</span>
              </div>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-fg-muted">BND addr</span>
              <div className="flex items-center gap-2">
                <Badge variant={stateVariant(selftest.bnd.addrState)}>
                  {stateIcon(selftest.bnd.addrState)} {selftest.bnd.addrState}
                </Badge>
                {selftest.bnd.lastAddr && (
                  <span className="text-xs font-mono text-fg-muted">{selftest.bnd.lastAddr}</span>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ME Quality counters */}
      {meQuality?.enabled && (
        <div className="rounded-xs bg-bg-card p-4 flex flex-col gap-2">
          <FieldLabel>ME Quality</FieldLabel>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
            <span className="text-fg-muted">KDF Drift</span>
            <MonoValue>{meQuality.counters.kdfDriftTotal}</MonoValue>
            <span className="text-fg-muted">KDF Port-only Drift</span>
            <MonoValue>{meQuality.counters.kdfPortOnlyDriftTotal}</MonoValue>
            <span className="text-fg-muted">Route Drops: no_conn</span>
            <span
              className={`font-mono ${meQuality.counters.routeDropNoConn > 0 ? "text-status-warn" : "text-fg"}`}
            >
              {meQuality.counters.routeDropNoConn}
            </span>
            <span className="text-fg-muted">Route Drops: channel_closed</span>
            <MonoValue>{meQuality.counters.routeDropChannelClosed}</MonoValue>
            <span className="text-fg-muted">Route Drops: queue_full</span>
            <MonoValue>{meQuality.counters.routeDropQueueFull}</MonoValue>
            <span className="text-fg-muted">Reconnect attempts</span>
            <MonoValue>{meQuality.counters.reconnectAttemptTotal}</MonoValue>
            <span className="text-fg-muted">Reconnect success</span>
            <span className="font-mono text-xs text-fg">
              {meQuality.counters.reconnectSuccessTotal}
              {meQuality.counters.reconnectAttemptTotal > 0 && (
                <span className="text-fg-muted ml-1">
                  (
                  {(
                    (meQuality.counters.reconnectSuccessTotal /
                      meQuality.counters.reconnectAttemptTotal) *
                    100
                  ).toFixed(1)}
                  %)
                </span>
              )}
            </span>
          </div>
        </div>
      )}

      {/* NAT / STUN */}
      {natStun?.enabled && (
        <div className="rounded-xs bg-bg-card p-4 flex flex-col gap-2">
          <FieldLabel>NAT / STUN</FieldLabel>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
            <span className="text-fg-muted">NAT Probe</span>
            <Badge variant={natStun.natProbeEnabled ? "ok" : "default"}>
              {natStun.natProbeEnabled ? "Enabled" : "Disabled"}
            </Badge>
            <span className="text-fg-muted">Live STUNs</span>
            <MonoValue>
              {natStun.liveStunTotal}/{natStun.configuredStunTotal}
            </MonoValue>
            <span className="text-fg-muted">Public v4</span>
            <MonoValue>
              {natStun.reflectionV4
                ? `${natStun.reflectionV4.addr} (${Math.round(natStun.reflectionV4.ageSecs / 60)}min ago)`
                : "—"}
            </MonoValue>
            <span className="text-fg-muted">Public v6</span>
            <MonoValue>
              {natStun.reflectionV6
                ? `${natStun.reflectionV6.addr} (${Math.round(natStun.reflectionV6.ageSecs / 60)}min ago)`
                : "—"}
            </MonoValue>
          </div>
          {natStun.configuredServers.length > 0 && (
            <div className="flex flex-col gap-1 mt-1">
              <FieldLabel>Configured Servers</FieldLabel>
              <div className="flex flex-wrap gap-1">
                {natStun.configuredServers.map((addr) => (
                  <Badge key={addr} variant="default">
                    <MonoValue>{addr}</MonoValue>
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Network Path */}
      {networkPath && networkPath.length > 0 && (
        <div className="flex flex-col gap-2">
          <SectionHeader title="Network Path" />
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2">
            {networkPath.map((entry) => (
              <div key={entry.dc} className="rounded-xs bg-bg-card p-3 flex flex-col gap-1">
                <span className="text-section">DC {entry.dc}</span>
                {entry.ipPreference && (
                  <span className="text-xs text-fg-muted font-mono">
                    ip preference: {entry.ipPreference}
                  </span>
                )}
                {entry.selectedAddrV4 && (
                  <span className="text-xs font-mono text-fg">v4: {entry.selectedAddrV4}</span>
                )}
                {entry.selectedAddrV6 && (
                  <span className="text-xs font-mono text-fg">v6: {entry.selectedAddrV6}</span>
                )}
                {!entry.selectedAddrV4 && !entry.selectedAddrV6 && (
                  <span className="text-xs text-fg-muted">No addresses</span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* System Info */}
      <div className="rounded-xs bg-bg-card p-4 flex flex-col gap-2">
        <FieldLabel>System Info</FieldLabel>
        <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
          <span className="text-fg-muted">Version</span>
          <MonoValue>{systemInfo.version}</MonoValue>
          <span className="text-fg-muted">Arch / OS</span>
          <MonoValue>
            {systemInfo.targetArch} / {systemInfo.targetOs}
          </MonoValue>
          <span className="text-fg-muted">Build</span>
          <MonoValue>{systemInfo.buildProfile}</MonoValue>
          {systemInfo.gitCommit && (
            <>
              <span className="text-fg-muted">Git commit</span>
              <MonoValue>{systemInfo.gitCommit.slice(0, 8)}</MonoValue>
            </>
          )}
          {systemInfo.buildTimeUtc && (
            <>
              <span className="text-fg-muted">Build time</span>
              <MonoValue>{systemInfo.buildTimeUtc}</MonoValue>
            </>
          )}
          <span className="text-fg-muted">Config path</span>
          <MonoValue className="truncate">{systemInfo.configPath}</MonoValue>
          <span className="text-fg-muted">Config reloads</span>
          <span className="font-mono text-xs text-fg">
            {systemInfo.configReloadCount}
            {systemInfo.lastConfigReloadEpochSecs && (
              <span className="text-fg-muted ml-1">
                (last: {formatTime(systemInfo.lastConfigReloadEpochSecs)})
              </span>
            )}
          </span>
        </div>
      </div>
    </div>
  );
}
