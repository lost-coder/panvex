import { useTranslation } from "react-i18next";

import {
  FieldLabel,
  MonoValue,
  Sheet,
  SheetBody,
  SheetContent,
  SheetHeader,
  SheetTitle,
  coverageColor,
} from "@/ui";
import type { ServerDcData } from "@/shared/api/types-pages/pages";

function rttClass(rttMs: number): string {
  if (rttMs > 300) return "text-status-error";
  if (rttMs > 100) return "text-status-warn";
  return "text-fg";
}

/**
 * Bottom sheet that opens from the mobile DC scroll-strip, the desktop
 * radar, and the desktop tile grid. The Sheet root and SheetContent both
 * forward `onOpenChange` so a backdrop tap correctly clears `selectedDc`
 * (otherwise SheetContent's backdrop would be a dead overlay trapping
 * clicks).
 */
export function DcDetailSheet({
  selectedDc,
  onClose,
}: Readonly<{
  selectedDc: ServerDcData | null;
  onClose: () => void;
}>) {
  const { t } = useTranslation("servers");
  return (
    <Sheet
      open={selectedDc !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="bottom"
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      >
        {selectedDc && (
          <>
            <SheetHeader>
              <SheetTitle>{t("detail.dcSheet.title", { dc: selectedDc.dc })}</SheetTitle>
            </SheetHeader>
            <SheetBody>
              <div className="flex flex-col gap-4">
                <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
                  <span className="text-fg-muted">{t("detail.dcSheet.coverage")}</span>
                  <span
                    className={`font-mono font-semibold ${coverageColor(selectedDc.coveragePct)}`}
                  >
                    {selectedDc.coveragePct}%
                  </span>
                  <span className="text-fg-muted">{t("detail.dcSheet.available")}</span>
                  <span
                    className={`font-mono ${selectedDc.availablePct < 100 ? "text-status-warn" : "text-fg"}`}
                  >
                    {selectedDc.availablePct}%
                  </span>
                  <span className="text-fg-muted">{t("detail.dcSheet.writers")}</span>
                  <span className="font-mono text-fg">
                    {t("detail.dcSheet.writersAlive", {
                      alive: selectedDc.aliveWriters,
                      required: selectedDc.requiredWriters,
                    })}
                  </span>
                  <span className="text-fg-muted">{t("detail.dcSheet.rtt")}</span>
                  <span
                    className={`font-mono ${rttClass(selectedDc.rttMs ?? 0)}`}
                  >
                    {selectedDc.rttMs == null ? "—" : `${selectedDc.rttMs}ms`}
                  </span>
                  <span className="text-fg-muted">{t("detail.dcSheet.load")}</span>
                  <span className="font-mono text-fg">{selectedDc.load}</span>
                  <span className="text-fg-muted">{t("detail.dcSheet.floor")}</span>
                  <span className="font-mono text-fg">
                    {t("detail.dcSheet.floorRange", {
                      min: selectedDc.floorMin,
                      target: selectedDc.floorTarget,
                      max: selectedDc.floorMax,
                    })}
                    {selectedDc.floorCapped && (
                      <span className="text-status-warn ml-1">{"⚠ "}{t("detail.dcSheet.capped")}</span>
                    )}
                  </span>
                </div>

                {selectedDc.endpointWriters.length > 0 && (
                  <div className="flex flex-col gap-2">
                    <FieldLabel>{t("detail.dcSheet.endpointsAndWriters")}</FieldLabel>
                    {selectedDc.endpointWriters.map((ew) => (
                      <div key={ew.endpoint} className="flex items-center gap-2 text-sm">
                        <MonoValue>{ew.endpoint}</MonoValue>
                        <span className="text-fg-muted">→</span>
                        <MonoValue>
                          {t("detail.dcSheet.activeWriters", { count: ew.activeWriters })}
                        </MonoValue>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </SheetBody>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
