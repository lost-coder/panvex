import { useState } from "react";
import { PageHeader } from "@/ui/layout/PageHeader";
import { SettingsGroup } from "@/ui/components/SettingsGroup";
import { SettingsRow } from "@/ui/components/SettingsRow";
import { Select } from "@/ui/base/select";
import { Button } from "@/ui/base/button";
import { Sheet, SheetContent, SheetBody } from "@/ui/base/sheet";
import { Badge } from "@/ui/primitives/Badge";
import { TotpSetupSheet } from "@/features/auth/TotpSetupSheet";
import { TotpDisableSheet } from "@/features/auth/TotpDisableSheet";
import type { ProfilePageProps, TotpSetupData } from "@/shared/api/types-pages/pages";

export function ProfilePage({
  user,
  appearance,
  onAppearanceChange,
  onStartTotpSetup,
  onEnableTotp,
  onDisableTotp,
  totpSetupLoading,
  totpEnableLoading,
  totpDisableLoading,
  totpError,
}: ProfilePageProps) {
  const initials = user.username.charAt(0).toUpperCase();
  const [setupOpen, setSetupOpen] = useState(false);
  const [disableOpen, setDisableOpen] = useState(false);
  const [setupData, setSetupData] = useState<TotpSetupData | null>(null);

  async function handleStartSetup() {
    if (!onStartTotpSetup) return;
    try {
      const data = await onStartTotpSetup();
      setSetupData(data);
      setSetupOpen(true);
    } catch {
      // Error handling is done by the container
    }
  }

  return (
    <div className="flex flex-col">
      <PageHeader title="Profile" subtitle="Account information and preferences" />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8">
        {/* User info card */}
        <div className="rounded-xs bg-bg-card border border-border p-6 flex flex-col items-center gap-3 text-center">
          {/* Avatar */}
          <div className="w-16 h-16 rounded-full bg-accent/15 flex items-center justify-center">
            <span className="text-2xl font-mono font-bold text-accent">{initials}</span>
          </div>

          {/* Username */}
          <span className="text-lg font-semibold text-fg tracking-tight">{user.username}</span>

          {/* Role + TOTP badges */}
          <div className="flex items-center gap-2 flex-wrap justify-center">
            <Badge variant="accent">{user.role}</Badge>
            {user.totpEnabled ? (
              <Badge variant="ok">2FA Enabled</Badge>
            ) : (
              <Badge variant="warn">2FA Disabled</Badge>
            )}
          </div>
        </div>

        {/* Appearance */}
        <SettingsGroup title="Appearance">
          <SettingsRow label="Theme">
            <Select
              className="w-36"
              value={appearance.theme}
              options={[
                { value: "system", label: "System" },
                { value: "light", label: "Light" },
                { value: "dark", label: "Dark" },
              ]}
              onChange={(v) =>
                onAppearanceChange?.({
                  ...appearance,
                  theme: v as typeof appearance.theme,
                })
              }
            />
          </SettingsRow>
          <SettingsRow label="Density">
            <Select
              className="w-36"
              value={appearance.density}
              options={[
                { value: "comfortable", label: "Comfortable" },
                { value: "compact", label: "Compact" },
              ]}
              onChange={(v) =>
                onAppearanceChange?.({
                  ...appearance,
                  density: v as typeof appearance.density,
                })
              }
            />
          </SettingsRow>
          <SettingsRow label="Swipe Navigation" description="Swipe between pages on mobile">
            <input
              type="checkbox"
              className="h-4 w-4 accent-[var(--color-accent)] cursor-pointer"
              checked={appearance.swipeNavigation}
              onChange={(e) =>
                onAppearanceChange?.({
                  ...appearance,
                  swipeNavigation: e.target.checked,
                })
              }
            />
          </SettingsRow>
        </SettingsGroup>

        {/* Security */}
        <SettingsGroup title="Security">
          <SettingsRow
            label="Two-Factor Authentication"
            description={
              user.totpEnabled
                ? "Your account is protected with 2FA"
                : "Add an extra layer of security to your account"
            }
          >
            {user.totpEnabled ? (
              <Button
                size="sm"
                variant="outline"
                onClick={() => setDisableOpen(true)}
                disabled={!onDisableTotp}
              >
                Disable 2FA
              </Button>
            ) : (
              <Button
                size="sm"
                onClick={handleStartSetup}
                disabled={!onStartTotpSetup || totpSetupLoading}
              >
                {totpSetupLoading ? "Loading..." : "Set Up 2FA"}
              </Button>
            )}
          </SettingsRow>
        </SettingsGroup>
      </div>

      {/* TOTP Setup Sheet */}
      {setupData && onEnableTotp && (
        <Sheet
          open={setupOpen}
          onOpenChange={(open) => {
            if (!open) {
              setSetupOpen(false);
              setSetupData(null);
            }
          }}
        >
          <SheetContent side="bottom">
            <SheetBody>
              <TotpSetupSheet
                secret={setupData.secret}
                otpauthUrl={setupData.otpauthUrl}
                onEnable={async (password, code) => {
                  try {
                    await onEnableTotp(password, code);
                    setSetupOpen(false);
                  } catch {
                    // Sheet stays open, error displayed via totpError prop
                  }
                }}
                onCancel={() => setSetupOpen(false)}
                loading={totpEnableLoading}
                error={totpError}
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}

      {/* TOTP Disable Sheet */}
      {onDisableTotp && (
        <Sheet
          open={disableOpen}
          onOpenChange={(open) => {
            if (!open) setDisableOpen(false);
          }}
        >
          <SheetContent side="bottom">
            <SheetBody>
              <TotpDisableSheet
                onDisable={async (password, code) => {
                  try {
                    await onDisableTotp(password, code);
                    setDisableOpen(false);
                  } catch {
                    // Sheet stays open, error displayed via totpError prop
                  }
                }}
                onCancel={() => setDisableOpen(false)}
                loading={totpDisableLoading}
                error={totpError}
              />
            </SheetBody>
          </SheetContent>
        </Sheet>
      )}
    </div>
  );
}
