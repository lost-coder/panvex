import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";

import { SkeletonRows } from "@/ui";
import { useToast } from "@/app/providers/ToastProvider";
import { FleetGroupsPage } from "./FleetGroupsPage";
import { type FleetGroupFormData } from "./FleetGroupFormSheet";
import {
  useFleetGroupMutations,
  useFleetGroupsList,
} from "./hooks/useFleetGroupsFull";

type SheetState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; id: string };

const emptyForm: FleetGroupFormData = { name: "", label: "", description: "" };

export function FleetGroupsContainer() {
  const navigate = useNavigate();
  const toast = useToast();
  const { data: groups, isLoading } = useFleetGroupsList();
  const { createMutation, updateMutation } = useFleetGroupMutations();

  const [sheet, setSheet] = useState<SheetState>({ mode: "closed" });
  const [formData, setFormData] = useState<FleetGroupFormData>(emptyForm);
  const [formError, setFormError] = useState<string>("");

  const openCreate = () => {
    setFormData(emptyForm);
    setFormError("");
    setSheet({ mode: "create" });
  };

  const openEdit = (id: string) => {
    const row = groups?.find((g) => g.id === id);
    if (!row) return;
    setFormData({ name: row.name, label: row.label, description: row.description });
    setFormError("");
    setSheet({ mode: "edit", id });
  };

  const closeSheet = () => {
    setSheet({ mode: "closed" });
    setFormError("");
  };

  const handleSubmit = async () => {
    setFormError("");
    try {
      if (sheet.mode === "create") {
        const created = await createMutation.mutateAsync({
          name: formData.name,
          label: formData.label,
          description: formData.description,
        });
        toast.success(`Fleet group «${created.label}» created.`);
      } else if (sheet.mode === "edit") {
        await updateMutation.mutateAsync({
          id: sheet.id,
          payload: { label: formData.label, description: formData.description },
        });
        toast.success("Fleet group updated.");
      }
      closeSheet();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Request failed");
    }
  };

  if (isLoading) {
    return (
      <div className="p-4">
        <SkeletonRows count={6} />
      </div>
    );
  }

  return (
    <FleetGroupsPage
      groups={groups ?? []}
      sheet={sheet}
      formData={formData}
      formError={formError}
      onFormDataChange={setFormData}
      onCreate={openCreate}
      onEdit={openEdit}
      onOpenDetail={(id) => navigate({ to: "/fleet-groups/$fleetGroupId", params: { fleetGroupId: id } })}
      onSubmit={handleSubmit}
      onCancel={closeSheet}
      saving={createMutation.isPending || updateMutation.isPending}
    />
  );
}
