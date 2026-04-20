// P3-FE-01: Phase-7 redesign — full-width hero with avatar + identity meta,
// two-column sections (Appearance · Security) on desktop, stacked on mobile.
import { useState } from "react";
import { Palette, ShieldCheck } from "lucide-react";
import {
  Badge,
  Button,
  PageHeader,
  PageSection,
  Select,
  SettingsRow,
  Sheet,
  SheetBody,
  SheetContent,
} from "@/ui";
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
    } catch (err) {
      // API errors are already surfaced by the container via `totpError`;
      // log so unexpected failures (malformed response, render-time bug)
      // still leave a trace in the console rather than silently closing
      // the sheet.
      console.error("TOTP setup failed", err);
    }
  }

  return (
    <div className="flex flex-col">
      <PageHeader title="Profile" subtitle="Account information and preferences" />

      <div className="px-4 md:px-8 flex flex-col gap-6 pb-8">
        {/* Hero card — left-aligned so the avatar anchors the eye and the
            username reads across in one line with role + 2FA state. */}
        <div className="rounded-xs bg-bg-card border border-border p-5 md:p-6 flex items-center gap-5">
          <div className="h-16 w-16 rounded-full bg-accent/15 flex items-center justify-center shrink-0">
            <span className="text-2xl font-mono font-bold text-accent">{initials}</span>
          </div>
          <div className="flex flex-col gap-2 min-w-0">
            <span className="text-lg md:text-xl font-semibold text-fg tracking-tight truncate">
              {user.username}
            </span>
            <div className="flex items-center gap-2 flex-wrap">
              <Badge variant="accent">{user.role}</Badge>
              {user.totpEnabled ? (
                <Badge variant="ok">2FA Enabled</Badge>
              ) : (
                <Badge variant="warn">2FA Disabled</Badge>
              )}
              <span
                className="font-mono text-[10px] text-fg-muted truncate"
                title={user.id}
              >
                id {user.id.slice(0, 8)}…
              </span>
            </div>
          </div>
        </div>

        {/* 2-col on md so the short Security section sits next to Appearance
            instead of leaving the right half of the page empty. */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-6 gap-y-8 items-start">
          <PageSection
            icon={Palette}
            title="Appearance"
            description="Your personal dashboard preferences."
          >
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
          </PageSection>

          <PageSection
            icon={ShieldCheck}
            title="Security"
            description="Protect your account with a second factor."
            tone={user.totpEnabled ? "default" : "warn"}
          >
            <SettingsRow
              label="Two-Factor Authentication"
              description={
                user.totpEnabled
                  ? "Your account is protected with a TOTP authenticator."
                  : "Add an authenticator app to require a 6-digit code at sign-in."
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
                  {totpSetupLoading ? "Loading…" : "Set Up 2FA"}
                </Button>
              )}
            </SettingsRow>
          </PageSection>
        </div>
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
                  } catch (err) {
                    // API errors land in `totpError` and keep the sheet
                    // open intentionally. Log so unexpected throws still
                    // leave a trace.
                    console.error("TOTP enable failed", err);
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
                  } catch (err) {
                    // API errors land in `totpError` and keep the sheet
                    // open intentionally. Log so unexpected throws still
                    // leave a trace.
                    console.error("TOTP disable failed", err);
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
