import { useState } from "react";
import { Button } from "@/ui/base/button";
import { Input } from "@/ui/base/input";
import { FormField } from "@/ui/base/form-field";

interface TotpDisableSheetProps {
  onDisable: (password: string, totpCode: string) => Promise<void>;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
}

export function TotpDisableSheet({ onDisable, onCancel, loading, error }: TotpDisableSheetProps) {
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-title">Disable Two-Factor Authentication</h3>
        <p className="text-sm text-fg-muted mt-0.5">
          Enter your password and current authenticator code to disable 2FA.
        </p>
      </div>

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
          onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, ""))}
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
          variant="danger"
          onClick={() => onDisable(password, totpCode)}
          disabled={loading || !password || totpCode.length < 6}
        >
          {loading ? "Disabling..." : "Disable 2FA"}
        </Button>
      </div>
    </div>
  );
}
