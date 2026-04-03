import { useState } from "react";
import { useRouter } from "@tanstack/react-router";
import { LoginPage } from "@panvex/ui";
import { apiClient } from "@/lib/api";

export function LoginContainer() {
  const router = useRouter();
  const [stage, setStage] = useState<"credentials" | "totp">("credentials");
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [savedCredentials, setSavedCredentials] = useState<{ username: string; password: string }>();

  async function handleCredentials(username: string, password: string) {
    setError(undefined);
    setLoading(true);
    try {
      await apiClient.login({ username, password });
      router.navigate({ to: "/" });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.toLowerCase().includes("totp")) {
        setSavedCredentials({ username, password });
        setStage("totp");
        setError(undefined);
      } else {
        setError(msg || "Login failed.");
      }
    } finally {
      setLoading(false);
    }
  }

  async function handleTotp(totpCode: string) {
    if (!savedCredentials) return;
    setError(undefined);
    setLoading(true);
    try {
      await apiClient.login({ ...savedCredentials, totp_code: totpCode });
      router.navigate({ to: "/" });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setError(msg || "Invalid TOTP code.");
    } finally {
      setLoading(false);
    }
  }

  function handleBack() {
    setStage("credentials");
    setError(undefined);
  }

  return (
    <LoginPage
      onCredentials={handleCredentials}
      onTotp={handleTotp}
      onBack={handleBack}
      stage={stage}
      error={error}
      loading={loading}
    />
  );
}
