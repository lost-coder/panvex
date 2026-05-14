import { lazy, Suspense, useState } from "react";
import { useTranslation } from "react-i18next";
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
  const { t } = useTranslation("auth");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h3 className="text-title">{t("totp.setup.title")}</h3>
        <p className="text-sm text-fg-muted mt-0.5">
          {t("totp.setup.description")}
        </p>
      </div>

      {/* QR Code — loaded lazily (U8) */}
      <div className="flex flex-col items-center gap-3 p-4 rounded-xs bg-white">
        <Suspense
          fallback={
            <output
              className="flex items-center justify-center h-[180px] w-[180px] text-fg-muted text-xs"
              aria-label={t("totp.setup.qrLoading")}
            >
              …
            </output>
          }
        >
          <LazyQRCode value={otpauthUrl} size={180} level="M" />
        </Suspense>
      </div>

      {/* Manual secret */}
      <div>
        <FieldLabel>{t("totp.setup.manualKeyLabel")}</FieldLabel>
        <div className="flex items-center gap-2 mt-1">
          <MonoValue className="text-xs break-all">{secret}</MonoValue>
          <CopyButton text={secret} />
        </div>
      </div>

      {/* Verification */}
      <FormField label={t("totp.setup.passwordLabel")} variant="uppercase" required>
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder={t("totp.setup.passwordPlaceholder")}
          disabled={loading}
        />
      </FormField>

      <FormField label={t("totp.setup.codeLabel")} variant="uppercase" required>
        <Input
          value={totpCode}
          onChange={(e) => setTotpCode(e.target.value.replaceAll(/\D/g, ""))}
          inputMode="numeric"
          pattern="[0-9]*"
          placeholder={t("totp.setup.codePlaceholder")}
          maxLength={6}
          disabled={loading}
          className="font-mono tracking-widest"
        />
      </FormField>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <div className="flex gap-2 justify-end mt-2">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          {t("totp.setup.cancel")}
        </Button>
        <Button
          onClick={() => onEnable(password, totpCode)}
          disabled={loading || !password || totpCode.length < 6}
        >
          {loading ? t("totp.setup.submitLoading") : t("totp.setup.submit")}
        </Button>
      </div>
    </div>
  );
}
