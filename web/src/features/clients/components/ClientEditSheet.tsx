// R-Q-08: Edit-client sheet extracted from ClientDetailPage.tsx.
// Owns only open/close transitions on its props; form state stays
// with the host page (which seeds it from the latest server snapshot
// each time the sheet opens).

import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import {
  Sheet,
  SheetBody,
  SheetContent,
  type ClientAgentOption,
  type ClientFormData,
  type FleetGroupOption,
} from "@/ui";

export interface ClientEditSheetProps {
  open: boolean;
  data: ClientFormData;
  onChange: (data: ClientFormData) => void;
  onSubmit: () => void | Promise<void>;
  onClose: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
  fleetGroups?: FleetGroupOption[] | undefined;
  agents?: ClientAgentOption[] | undefined;
}

export function ClientEditSheet({
  open,
  data,
  onChange,
  onSubmit,
  onClose,
  loading,
  error,
  fleetGroups,
  agents,
}: ClientEditSheetProps) {
  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <SheetContent
        side="bottom"
        title="Edit client"
        onOpenChange={(next) => {
          if (!next) onClose();
        }}
      >
        <SheetBody>
          <ClientFormSheet
            mode="edit"
            data={data}
            onChange={onChange}
            onSubmit={onSubmit}
            onCancel={onClose}
            loading={loading}
            error={error}
            fleetGroups={fleetGroups}
            agents={agents}
          />
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
