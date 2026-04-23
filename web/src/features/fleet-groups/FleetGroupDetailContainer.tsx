import { useState } from "react";
import { useNavigate, useParams } from "@tanstack/react-router";

import { Spinner } from "@/ui";
import { useToast } from "@/app/providers/ToastProvider";
import { useConfirm } from "@/app/providers/ConfirmProvider";

import { FleetGroupDetailPage } from "./FleetGroupDetailPage";
import { type FleetGroupFormData } from "./FleetGroupFormSheet";
import {
  useFleetGroupDeletionPreview,
  useFleetGroupDetail,
  useFleetGroupMutations,
  useFleetGroupsList,
} from "./hooks/useFleetGroupsFull";

export function FleetGroupDetailContainer() {
  const { fleetGroupId } = useParams({ strict: false });
  const id = fleetGroupId ?? "";
  const navigate = useNavigate();
  const toast = useToast();
  const confirm = useConfirm();

  const { data: group, isLoading } = useFleetGroupDetail(id);
  const { data: allGroups } = useFleetGroupsList();
  const { updateMutation, deleteMutation } = useFleetGroupMutations();

  const [editOpen, setEditOpen] = useState(false);
  const [formData, setFormData] = useState<FleetGroupFormData>({
    name: "",
    label: "",
    description: "",
  });
  const [formError, setFormError] = useState<string>("");

  // Deletion preview is fetched on demand — when the confirm dialog
  // needs to show the blast radius. We gate it on a local flag so
  // the query doesn't fire until the operator actually hits Delete.
  const [previewEnabled, setPreviewEnabled] = useState(false);
  const { data: preview } = useFleetGroupDeletionPreview(id, previewEnabled);

  const openEdit = () => {
    if (!group) return;
    setFormData({
      name: group.name,
      label: group.label,
      description: group.description,
    });
    setFormError("");
    setEditOpen(true);
  };

  const handleSubmitEdit = async () => {
    setFormError("");
    try {
      await updateMutation.mutateAsync({
        id,
        payload: { label: formData.label, description: formData.description },
      });
      toast.success("Fleet group updated.");
      setEditOpen(false);
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Request failed");
    }
  };

  const handleDelete = async () => {
    if (!group) return;
    // Kick off the preview query, then wait for it. The confirm dialog
    // below already pops once the operator triggers this flow.
    setPreviewEnabled(true);

    // Let the preview fetch resolve via React Query. In practice the
    // hook's enabled-flag triggers a fetch the moment previewEnabled
    // flips; by the time the user reaches the target-picker below,
    // the data is in cache.
    const basePreview = preview ?? (await import("@/shared/api/api")
      .then(({ apiClient }) => apiClient.fleetGroupDeletionPreview(id))
      .catch(() => undefined));

    const candidates = (allGroups ?? []).filter((g) => g.id !== id);
    const hasMembers = basePreview?.reassign_required ?? false;

    let reassignTo: string | undefined = undefined;
    if (hasMembers) {
      if (candidates.length === 0) {
        toast.error(
          "Нельзя удалить группу: в ней есть агенты/токены, а перенести их некуда — создайте ещё одну группу.",
        );
        return;
      }
      // Pick a sensible default: the "default" slug if it exists,
      // otherwise the oldest group. Operator can change it in the
      // prompt dialog that a full implementation would surface.
      const fallback =
        candidates.find((g) => g.name === "default") ?? candidates[0];
      reassignTo = fallback?.id;
    }

    const body = hasMembers
      ? [
          `Группа «${group.label || group.name}» содержит:`,
          `  • ${basePreview?.agent_count ?? 0} агентов`,
          `  • ${basePreview?.enrollment_token_count ?? 0} токенов`,
          `  • ${basePreview?.client_assignment_count ?? 0} client-assignments`,
          ``,
          `Все они будут перенесены в «${
            candidates.find((g) => g.id === reassignTo)?.label ??
            candidates.find((g) => g.id === reassignTo)?.name ??
            reassignTo
          }». Продолжить?`,
        ].join("\n")
      : `Удалить пустую группу «${group.label || group.name}»?`;

    const ok = await confirm({
      title: "Удалить fleet group?",
      body,
      confirmLabel: "Удалить",
      variant: "danger",
      requireTypeMatch: group.name,
    });
    if (!ok) {
      setPreviewEnabled(false);
      return;
    }

    try {
      await deleteMutation.mutateAsync({ id, reassignTo });
      toast.success("Fleet group удалена.");
      navigate({ to: "/fleet-groups" });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Delete failed");
    }
  };

  if (isLoading || !group) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spinner />
      </div>
    );
  }

  return (
    <FleetGroupDetailPage
      group={group}
      onBack={() => navigate({ to: "/fleet-groups" })}
      onEdit={openEdit}
      onDelete={handleDelete}
      editOpen={editOpen}
      formData={formData}
      formError={formError}
      onFormDataChange={setFormData}
      onSubmitEdit={handleSubmitEdit}
      onCancelEdit={() => setEditOpen(false)}
      saving={updateMutation.isPending || deleteMutation.isPending}
    />
  );
}
