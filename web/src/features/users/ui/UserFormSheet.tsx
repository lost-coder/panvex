import { useTranslation } from "react-i18next";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetBody } from "@/ui/base/sheet";
import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { Select } from "@/ui/base/select";
import { FormField } from "@/ui/base/form-field";
import type { UserFormSheetProps } from "@/shared/api/types-pages/pages";

export function UserFormSheet({
  mode,
  data,
  onChange,
  onSubmit,
  onCancel,
  loading,
  error,
}: Readonly<UserFormSheetProps>) {
  const { t } = useTranslation("users");

  function update<K extends keyof typeof data>(key: K, value: (typeof data)[K]) {
    onChange({ ...data, [key]: value });
  }

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <SheetContent>
        <SheetHeader>
          <SheetTitle>{mode === "create" ? t("form.addTitle") : t("form.editTitle")}</SheetTitle>
        </SheetHeader>
        <SheetBody>
          <div className="flex flex-col gap-4">
            <p className="text-sm text-fg-muted">{t("form.description")}</p>

            <FormField label={t("form.usernameLabel")} variant="uppercase" required>
              <Input
                value={data.username}
                onChange={(e) => update("username", e.target.value)}
                placeholder={t("form.usernamePlaceholder")}
                disabled={loading || mode === "edit"}
                autoComplete="off"
              />
            </FormField>

            <FormField
              label={mode === "create" ? t("form.passwordLabel") : t("form.passwordLabelKeep")}
              variant="uppercase"
              required={mode === "create"}
            >
              <Input
                type="password"
                value={data.password}
                onChange={(e) => update("password", e.target.value)}
                placeholder={mode === "edit" ? t("form.passwordPlaceholderKeep") : ""}
                disabled={loading}
                autoComplete="new-password"
              />
            </FormField>

            <FormField label={t("form.roleLabel")} variant="uppercase" required>
              <Select
                value={data.role}
                options={[
                  { value: "admin", label: t("form.roleAdmin") },
                  { value: "operator", label: t("form.roleOperator") },
                  { value: "viewer", label: t("form.roleViewer") },
                ]}
                onChange={(v) => update("role", v as typeof data.role)}
              />
            </FormField>

            {error && <div className="text-xs text-status-error">{error}</div>}

            <div className="flex gap-2 justify-end mt-2">
              <Button variant="ghost" onClick={onCancel} disabled={loading}>
                {t("form.cancel")}
              </Button>
              <Button
                onClick={onSubmit}
                disabled={loading || !data.username || (mode === "create" && !data.password)}
              >
                {(() => {
                  if (loading) return t("form.saving");
                  if (mode === "create") return t("form.submitAdd");
                  return t("form.submitSave");
                })()}
              </Button>
            </div>
          </div>
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
