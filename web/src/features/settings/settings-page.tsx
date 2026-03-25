import { SectionPanel } from "@/components/section-panel";
import { Button } from "@/components/ui/button";
import { Settings, Users, Shield } from "lucide-react";
import { usePanelSettings, useUsers } from "./settings-state";

export function SettingsPage() {
  const { data: settings, isLoading } = usePanelSettings();
  const { data: users = [] } = useUsers();

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        {[...Array(3)].map((_, i) => (
          <div key={i} className="animate-pulse bg-surface h-32 rounded" />
        ))}
      </div>
    );
  }

  return (
    <div className="p-6 space-y-4 max-w-3xl">
      <div>
        <h1 className="text-xl font-bold text-text-1">Settings</h1>
        <p className="text-sm text-text-3 mt-0.5">Manage panel configuration</p>
      </div>

      <SectionPanel title="Panel Settings" icon={<Settings className="w-4 h-4" />}>
        <div className="p-4 space-y-3">
          <div className="flex items-center justify-between py-2 border-b border-border">
            <div>
              <p className="text-sm font-semibold text-text-1">Public URL</p>
              <p className="text-xs text-text-3 mt-0.5">{settings?.http_public_url ?? "—"}</p>
            </div>
          </div>
          <div className="flex items-center justify-between py-2">
            <div>
              <p className="text-sm font-semibold text-text-1">gRPC Endpoint</p>
              <p className="text-xs text-text-3 mt-0.5">{settings?.grpc_public_endpoint ?? "—"}</p>
            </div>
            <Button variant="secondary" size="sm">Edit</Button>
          </div>
        </div>
      </SectionPanel>

      <SectionPanel title="User Management" icon={<Users className="w-4 h-4" />}>
        <div className="divide-y divide-border">
          {users.length === 0 ? (
            <p className="p-4 text-sm text-text-3">No users found.</p>
          ) : (
            users.map((user: any) => (
              <div key={user.id} className="flex items-center justify-between px-4 py-3">
                <div>
                  <p className="text-sm font-semibold text-text-1">{user.username ?? user.name}</p>
                  <p className="text-xs text-text-3">{user.role ?? "user"}</p>
                </div>
                <Button variant="ghost" size="sm">Edit</Button>
              </div>
            ))
          )}
        </div>
        <div className="px-4 py-3 border-t border-border">
          <Button variant="secondary" size="sm">Add User</Button>
        </div>
      </SectionPanel>

      <SectionPanel title="Security" icon={<Shield className="w-4 h-4" />}>
        <div className="p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-semibold text-text-1">Two-Factor Authentication</p>
              <p className="text-xs text-text-3 mt-0.5">Protect your account with TOTP</p>
            </div>
            <Button variant="secondary" size="sm">Configure</Button>
          </div>
        </div>
      </SectionPanel>
    </div>
  );
}
