// R-Q-08: Add-client sheet extracted from ClientsPage.tsx. Owns only
// open/close transitions on its props — form state stays with the host.

import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useConfirm } from "@/app/providers/ConfirmProvider";
import { ClientFormSheet } from "@/features/clients/ClientFormSheet";
import { useUnsavedChangesGuard } from "@/shared/hooks";
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
  const { t } = useTranslation("clients");
  const { t: tc } = useTranslation("common");
  const confirm = useConfirm();

  // Snapshot the form at open time; dirty = anything diverged since.
  // useState (not useRef) so the snapshot is readable during render without
  // violating react-hooks/refs.
  const [initialSnapshot, setInitialSnapshot] = useState(() =>
    JSON.stringify(data),
  );
  const prevOpenRef = useRef(open);
  useEffect(() => {
    if (open && !prevOpenRef.current) setInitialSnapshot(JSON.stringify(data));
    prevOpenRef.current = open;
  }, [open, data]);
  const dirty = open && JSON.stringify(data) !== initialSnapshot;

  // Route-level + beforeunload protection while the sheet is dirty.
  useUnsavedChangesGuard(dirty);

  const requestClose = async () => {
    if (dirty) {
      const leave = await confirm({
        title: tc("unsaved.title"),
        body: tc("unsaved.body"),
        confirmLabel: tc("unsaved.leave"),
        cancelLabel: tc("unsaved.stay"),
        variant: "danger",
      });
      if (!leave) return;
    }
    onClose();
  };

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) void requestClose();
      }}
    >
      <SheetContent
        side="bottom"
        title={t("detail.addSheetTitle")}
        onOpenChange={(next) => {
          if (!next) void requestClose();
        }}
      >
        <SheetBody>
          <ClientFormSheet
            mode="create"
            data={data}
            onChange={onChange}
            onSubmit={onSubmit}
            onCancel={() => void requestClose()}
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
