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
}: UserFormSheetProps) {
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
          <SheetTitle>{mode === "create" ? "Add User" : "Edit User"}</SheetTitle>
        </SheetHeader>
        <SheetBody>
          <div className="flex flex-col gap-4">
            <p className="text-sm text-fg-muted">Panel administrator account.</p>

            <FormField label="Username" variant="uppercase" required>
              <Input
                value={data.username}
                onChange={(e) => update("username", e.target.value)}
                placeholder="admin"
                disabled={loading || mode === "edit"}
                autoComplete="off"
              />
            </FormField>

            <FormField
              label={mode === "create" ? "Password" : "Password (leave blank to keep)"}
              variant="uppercase"
              required={mode === "create"}
            >
              <Input
                type="password"
                value={data.password}
                onChange={(e) => update("password", e.target.value)}
                placeholder={mode === "edit" ? "••••••••" : ""}
                disabled={loading}
                autoComplete="new-password"
              />
            </FormField>

            <FormField label="Role" variant="uppercase" required>
              <Select
                value={data.role}
                options={[
                  { value: "admin", label: "Admin — Full access" },
                  { value: "operator", label: "Operator — Manage nodes & clients" },
                  { value: "viewer", label: "Viewer — Read-only" },
                ]}
                onChange={(v) => update("role", v as typeof data.role)}
              />
            </FormField>

            {error && <div className="text-xs text-status-error">{error}</div>}

            <div className="flex gap-2 justify-end mt-2">
              <Button variant="ghost" onClick={onCancel} disabled={loading}>
                Cancel
              </Button>
              <Button
                onClick={onSubmit}
                disabled={loading || !data.username || (mode === "create" && !data.password)}
              >
                {loading ? "Saving..." : mode === "create" ? "Add User" : "Save Changes"}
              </Button>
            </div>
          </div>
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
