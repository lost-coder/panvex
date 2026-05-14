import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { FormField } from "@/ui/base/form-field";

interface TotpDisableSheetProps {
  onDisable: (password: string, totpCode: string) => Promise<void>;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
}

export function TotpDisableSheet({ onDisable, onCancel, loading, error }: Readonly<TotpDisableSheetProps>) {
  const { t } = useTranslation("auth");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-title">{t("totp.disable.title")}</h3>
        <p className="text-sm text-fg-muted mt-0.5">
          {t("totp.disable.description")}
        </p>
      </div>

      <FormField label={t("totp.disable.passwordLabel")} variant="uppercase" required>
        <Input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder={t("totp.disable.passwordPlaceholder")}
          disabled={loading}
        />
      </FormField>

      <FormField label={t("totp.disable.codeLabel")} variant="uppercase" required>
        <Input
          value={totpCode}
          onChange={(e) => setTotpCode(e.target.value.replaceAll(/\D/g, ""))}
          inputMode="numeric"
          pattern="[0-9]*"
          placeholder={t("totp.disable.codePlaceholder")}
          maxLength={6}
          disabled={loading}
          className="font-mono tracking-widest"
        />
      </FormField>

      {error && <div className="text-xs text-status-error">{error}</div>}

      <div className="flex gap-2 justify-end mt-2">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          {t("totp.disable.cancel")}
        </Button>
        <Button
          variant="danger"
          onClick={() => onDisable(password, totpCode)}
          disabled={loading || !password || totpCode.length < 6}
        >
          {loading ? t("totp.disable.submitLoading") : t("totp.disable.submit")}
        </Button>
      </div>
    </div>
  );
}
