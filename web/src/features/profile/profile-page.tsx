import { SectionPanel } from "@/components/section-panel";
import { Button } from "@/components/ui/button";
import { Avatar } from "@/components/ui/avatar";
import { User, Palette, Shield } from "lucide-react";
import { useMe, useAppearanceSettings } from "./profile-state";

export function ProfilePage() {
  const { data: me, isLoading } = useMe();
  const { data: appearance } = useAppearanceSettings();

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <div className="animate-pulse bg-surface h-24 rounded" />
        <div className="animate-pulse bg-surface h-32 rounded" />
      </div>
    );
  }

  return (
    <div className="p-6 space-y-4 max-w-2xl">
      <div>
        <h1 className="text-xl font-bold text-text-1">Profile</h1>
        <p className="text-sm text-text-3 mt-0.5">Your account settings</p>
      </div>

      <SectionPanel title="Account" icon={<User className="w-4 h-4" />}>
        <div className="p-4">
          <div className="flex items-center gap-4">
            <Avatar name={me?.username ?? "?"} />
            <div>
              <p className="text-base font-bold text-text-1">{me?.username ?? "—"}</p>
              <p className="text-xs text-text-3 mt-0.5">{me?.role ?? "user"}</p>
            </div>
          </div>
          <div className="mt-4 pt-4 border-t border-border space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs text-text-3">User ID</span>
              <span className="text-xs font-mono text-text-2">{me?.id ?? "—"}</span>
            </div>
          </div>
        </div>
      </SectionPanel>

      <SectionPanel title="Appearance" icon={<Palette className="w-4 h-4" />}>
        <div className="p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-semibold text-text-1">Theme</p>
              <p className="text-xs text-text-3 mt-0.5">{appearance?.theme ?? "system"}</p>
            </div>
            <Button variant="secondary" size="sm">Change</Button>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-semibold text-text-1">Density</p>
              <p className="text-xs text-text-3 mt-0.5">{appearance?.density ?? "comfortable"}</p>
            </div>
            <Button variant="secondary" size="sm">Change</Button>
          </div>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-semibold text-text-1">Parameter Help</p>
              <p className="text-xs text-text-3 mt-0.5">{appearance?.help_mode ?? "basic"}</p>
            </div>
            <Button variant="secondary" size="sm">Change</Button>
          </div>
        </div>
      </SectionPanel>

      <SectionPanel title="Security" icon={<Shield className="w-4 h-4" />}>
        <div className="p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-semibold text-text-1">Two-Factor Authentication</p>
              <p className="text-xs text-text-3 mt-0.5">Add an extra layer of security</p>
            </div>
            <Button variant="secondary" size="sm">Setup</Button>
          </div>
        </div>
      </SectionPanel>
    </div>
  );
}
