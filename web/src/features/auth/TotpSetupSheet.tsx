import { lazy, Suspense, useState } from "react";
import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { FormField } from "@/ui/base/form-field";
import { MonoValue } from "@/ui/primitives/MonoValue";
import { CopyButton } from "@/ui/primitives/CopyButton";
import { FieldLabel } from "@/ui/primitives/FieldLabel";

// U8: qrcode.react (~20 kB) lands in its own chunk so the TOTP sheet
// only pulls the dependency when the sheet actually opens. Wrapped
// via React.lazy on a tiny default-export adapter in ./internal.
const LazyQRCode = lazy(() => import("@/ui/compositions/internal/QRCode"));

interface TotpSetupSheetProps {
  secret: string;
  otpauthUrl: string;
  onEnable: (password: string, totpCode: string) => Promise<void>;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
}

export function TotpSetupSheet({
  secret,
  otpauthUrl,
  onEnable,
  onCancel,
  loading,
  error,
}: Readonly<TotpSetupSheetProps>) {
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h3 className="text-title">Set Up Two-Factor Authentication</h3>
        <p className="text-sm text-fg-muted mt-0.5">
          Scan the QR code with your authenticator app, then enter your password and the generated
          code to verify.
        </p>
      </div>

      {/* QR Code — loaded lazily (U8) */}
      <div className="flex flex-col items-center gap-3 p-4 rounded-xs bg-white">
        <Suspense
          fallback={
            <div
              className="flex items-center justify-center h-[180px] w-[180px] text-fg-muted text-xs"
              role="status"
              aria-label="Loading QR code"
            >
              …
            </div>
          }
        >
          <LazyQRCode value={otpauthUrl} size={180} level="M" />
        </Suspense>
      </div>

      {/* Manual secret */}
      <div>
        <FieldLabel>Manual Entry Key</FieldLabel>
        <div className="flex items-center gap-2 mt-1">
          <MonoValue className="text-xs break-all">{secret}</MonoValue>
          <CopyButton text={secret} />
        </div>
      </div>

      {/* Verification */}
      <FormField label="Password" variant="uppercase" required>
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Your current password"
          disabled={loading}
        />
      </FormField>

      <FormField label="Authenticator Code" variant="uppercase" required>
        <Input
          value={totpCode}
          onChange={(e) => setTotpCode(e.target.value.replaceAll(/\D/g, ""))}
          inputMode="numeric"
          pattern="[0-9]*"
          placeholder="6-digit code"
          maxLength={6}
          disabled={loading}
          className="font-mono tracking-widest"
        />
      </FormField>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <div className="flex gap-2 justify-end mt-2">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          Cancel
        </Button>
        <Button
          onClick={() => onEnable(password, totpCode)}
          disabled={loading || !password || totpCode.length < 6}
        >
          {loading ? "Verifying..." : "Enable 2FA"}
        </Button>
      </div>
    </div>
  );
}
