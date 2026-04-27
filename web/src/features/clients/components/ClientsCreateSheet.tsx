// R-Q-08: Add-client sheet extracted from ClientsPage.tsx. Owns only
// open/close transitions on its props — form state stays with the host.

import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import {
  Sheet,
  SheetBody,
  SheetContent,
  type ClientAgentOption,
  type ClientFormData,
  type FleetGroupOption,
} from "@/ui";

export interface ClientsCreateSheetProps {
  open: boolean;
  data: ClientFormData;
  onChange: (data: Readonly<ClientFormData>) => void;
  onSubmit: () => void | Promise<void>;
  onClose: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
  fleetGroups?: FleetGroupOption[] | undefined;
  agents?: ClientAgentOption[] | undefined;
}

export function ClientsCreateSheet({
  open,
  data,
  onChange,
  onSubmit,
  onClose,
  loading,
  error,
  fleetGroups,
  agents,
}: Readonly<ClientsCreateSheetProps>) {
  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <SheetContent
        side="bottom"
        title="Add client"
        onOpenChange={(next) => {
          if (!next) onClose();
        }}
      >
        <SheetBody>
          <ClientFormSheet
            mode="create"
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
