import { useState } from "react";
import { useTranslation } from "react-i18next";
import { FieldLabel, MonoValue } from "@/ui/primitives";
import { coverageColor } from "@/ui/lib/status";
import type { ServerDcData } from "@/shared/api/types-pages/pages";

function rttClass(rttMs: number): string | undefined {
  if (rttMs > 300) return "text-status-error";
  if (rttMs > 100) return "text-status-warn";
  return undefined;
}

export function DcTable({ dcs }: Readonly<{ dcs: ServerDcData[] }>) {
  const { t } = useTranslation("servers");
  const [expandedDc, setExpandedDc] = useState<number | null>(null);

  return (
    <div className="rounded-xs border border-border overflow-hidden">
      {/* overflow-x-auto lets this dense 8-column DC table scroll
          horizontally on narrow viewports instead of crushing columns;
          min-width keeps each column readable while scrolling. */}
      <div className="overflow-x-auto">
        <table className="w-full min-w-[44rem] text-sm">
        <thead>
          <tr className="border-b border-border bg-bg-card">
            <th className="w-8 px-2 py-2" />
            <th className="px-3 py-2 text-left">
              <FieldLabel>{t("detail.dcTable.dc")}</FieldLabel>
            </th>
            <th className="px-3 py-2 text-left">
              <FieldLabel>{t("detail.dcTable.availablePct")}</FieldLabel>
            </th>
            <th className="px-3 py-2 text-left">
              <FieldLabel>{t("detail.dcTable.writers")}</FieldLabel>
            </th>
            <th className="px-3 py-2 text-left">
              <FieldLabel>{t("detail.dcTable.coveragePct")}</FieldLabel>
            </th>
            <th className="px-3 py-2 text-left">
              <FieldLabel>{t("detail.dcTable.freshPct")}</FieldLabel>
            </th>
            <th className="px-3 py-2 text-left">
              <FieldLabel>{t("detail.dcTable.rtt")}</FieldLabel>
            </th>
            <th className="px-3 py-2 text-left">
              <FieldLabel>{t("detail.dcTable.load")}</FieldLabel>
            </th>
          </tr>
        </thead>
        <tbody>
          {dcs.length === 0 && (
            <tr>
              <td colSpan={9} className="px-3 py-6 text-center text-fg-muted text-sm">
                {t("detail.dataCenters.noDc")}
              </td>
            </tr>
          )}
          {dcs.map((row) => (
            <>
              <tr
                key={row.dc}
                className="border-b border-border hover:bg-bg-hover cursor-pointer"
                onClick={() => setExpandedDc(expandedDc === row.dc ? null : row.dc)}
              >
                <td className="px-2 py-2 text-center text-fg-muted text-xs select-none">
                  {expandedDc === row.dc ? "▾" : "›"}
                </td>
                <td className="px-3 py-2">
                  <span className="font-mono text-xs font-semibold text-fg">{`DC${row.dc}`}</span>
                </td>
                <td className="px-3 py-2">
                  <MonoValue className={row.availablePct < 100 ? "text-status-warn" : undefined}>
                    {row.availablePct}%
                  </MonoValue>
                </td>
                <td className="px-3 py-2">
                  <MonoValue>
                    {row.aliveWriters}/{row.requiredWriters}
                  </MonoValue>
                </td>
                <td className="px-3 py-2">
                  <MonoValue className={`font-semibold ${coverageColor(row.coveragePct)}`}>
                    {row.coveragePct}%
                  </MonoValue>
                </td>
                <td className="px-3 py-2">
                  <MonoValue className="text-fg-muted">
                    {row.freshAlivePct == null ? "—" : `${row.freshAlivePct}%`}
                  </MonoValue>
                </td>
                <td className="px-3 py-2">
                  <MonoValue className={rttClass(row.rttMs ?? 0)}>
                    {row.rttMs == null ? "—" : `${row.rttMs}ms`}
                  </MonoValue>
                </td>
                <td className="px-3 py-2">
                  <MonoValue>{row.load}</MonoValue>
                </td>
              </tr>
              {expandedDc === row.dc && (
                <tr key={`${row.dc}-expanded`} className="border-b border-border bg-bg">
                  <td />
                  <td colSpan={7} className="px-4 py-4">
                    <div className="flex flex-col gap-4">
                      {/* Floor info */}
                      <div className="flex flex-wrap gap-4 text-xs">
                        <div className="flex flex-col gap-0.5">
                          <span className="text-label-compact">{t("detail.dcTable.floorPolicy")}</span>
                          <span className="font-mono text-fg">
                            {t("detail.dcTable.floorValue", {
                              min: row.floorMin,
                              target: row.floorTarget,
                              max: row.floorMax,
                            })}
                            {row.floorCapped && (
                              <span className="text-status-warn ml-2">{"⚠ "}{t("detail.dcTable.capped")}</span>
                            )}
                          </span>
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <span className="text-label-compact">{t("detail.dcTable.endpoints")}</span>
                          <MonoValue>
                            {t("detail.dcTable.endpointsAvailable", {
                              available: row.availableEndpoints,
                              total: row.endpoints.length,
                            })}
                          </MonoValue>
                        </div>
                      </div>
                      {/* Endpoint writers */}
                      <div className="flex flex-col gap-1">
                        <span className="text-label-compact">{t("detail.dcTable.endpointWriters")}</span>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-1">
                          {row.endpointWriters.length === 0 ? (
                            <span className="text-xs text-fg-muted">{t("detail.dcTable.noEndpointData")}</span>
                          ) : (
                            row.endpointWriters.map((ew) => (
                              <div
                                key={ew.endpoint}
                                className="flex items-center justify-between gap-3 px-3 py-1.5 rounded-xs bg-bg-card border border-border"
                              >
                                <MonoValue className="truncate">{ew.endpoint}</MonoValue>
                                <span
                                  className={`font-mono text-xs shrink-0 ${ew.activeWriters === 0 ? "text-status-warn" : "text-fg-muted"}`}
                                >
                                  {t("detail.dcTable.writers", { count: ew.activeWriters })}
                                  {ew.activeWriters === 0 && " ⚠"}
                                </span>
                              </div>
                            ))
                          )}
                        </div>
                      </div>
                    </div>
                  </td>
                </tr>
              )}
            </>
          ))}
        </tbody>
        </table>
      </div>
    </div>
  );
}
