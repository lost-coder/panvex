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
              <SheetTitle>DC{selectedDc.dc} Details</SheetTitle>
            </SheetHeader>
            <SheetBody>
              <div className="flex flex-col gap-4">
                <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
                  <span className="text-fg-muted">Coverage</span>
                  <span
                    className={`font-mono font-semibold ${coverageColor(selectedDc.coveragePct)}`}
                  >
                    {selectedDc.coveragePct}%
                  </span>
                  <span className="text-fg-muted">Available</span>
                  <span
                    className={`font-mono ${selectedDc.availablePct < 100 ? "text-status-warn" : "text-fg"}`}
                  >
                    {selectedDc.availablePct}%
                  </span>
                  <span className="text-fg-muted">Writers</span>
                  <span className="font-mono text-fg">
                    {selectedDc.aliveWriters}/{selectedDc.requiredWriters} alive
                  </span>
                  <span className="text-fg-muted">RTT</span>
                  <span
                    className={`font-mono ${rttClass(selectedDc.rttMs ?? 0)}`}
                  >
                    {selectedDc.rttMs == null ? "—" : `${selectedDc.rttMs}ms`}
                  </span>
                  <span className="text-fg-muted">Load</span>
                  <span className="font-mono text-fg">{selectedDc.load}</span>
                  <span className="text-fg-muted">Floor</span>
                  <span className="font-mono text-fg">
                    {selectedDc.floorMin}..{selectedDc.floorTarget}..{selectedDc.floorMax}
                    {selectedDc.floorCapped && (
                      <span className="text-status-warn ml-1">⚠ capped</span>
                    )}
                  </span>
                </div>

                {selectedDc.endpointWriters.length > 0 && (
                  <div className="flex flex-col gap-2">
                    <FieldLabel>Endpoints & Writers</FieldLabel>
                    {selectedDc.endpointWriters.map((ew) => (
                      <div key={ew.endpoint} className="flex items-center gap-2 text-sm">
                        <MonoValue>{ew.endpoint}</MonoValue>
                        <span className="text-fg-muted">→</span>
                        <MonoValue>
                          {ew.activeWriters} active writer{ew.activeWriters === 1 ? "" : "s"}
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
