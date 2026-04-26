import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useRouter } from "@tanstack/react-router";
import { LoginPage } from "@/features/auth/LoginPage";
import { apiClient, ApiError } from "@/shared/api/api";

// Backend-surfaced codes that mean "transient infrastructure hiccup,
// not a credential problem". We render a retry-friendly message for
// each so operators do not think their password is wrong and trip the
// lockout counter by re-entering it.
const TRANSIENT_LOGIN_CODES = new Set([
  "audit_persist_unavailable", // B1 — audit log could not be written in time
  "session_store_unavailable", // P2-SEC-07 — session table was briefly down
]);

export function LoginContainer() {
  const router = useRouter();
  const { t } = useTranslation("auth");
  const [stage, setStage] = useState<"credentials" | "totp">("credentials");
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(false);
  const [savedCredentials, setSavedCredentials] = useState<{ username: string; password: string }>();

  function loginErrorMessage(err: unknown): string {
    if (err instanceof ApiError && err.code && TRANSIENT_LOGIN_CODES.has(err.code)) {
      return t("errors.transient");
    }
    if (err instanceof Error) {
      return err.message || t("errors.loginFailed");
    }
    return String(err) || t("errors.loginFailed");
  }

  async function handleCredentials(username: string, password: string) {
    setError(undefined);
    setLoading(true);
    try {
      await apiClient.login({ username, password });
      router.navigate({ to: "/" });
    } catch (err: unknown) {
      if (err instanceof ApiError && (err.code === "totp_required" || err.code === "totp_invalid")) {
        setSavedCredentials({ username, password });
        setStage("totp");
        setError(undefined);
      } else {
        setError(loginErrorMessage(err));
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
      setSavedCredentials(undefined);
      router.navigate({ to: "/" });
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code && TRANSIENT_LOGIN_CODES.has(err.code)) {
        setError(loginErrorMessage(err));
      } else {
        const msg = err instanceof Error ? err.message : String(err);
        setError(msg || t("errors.invalidTotp"));
      }
    } finally {
      setLoading(false);
    }
  }

  function handleBack() {
    setStage("credentials");
    setError(undefined);
    setSavedCredentials(undefined);
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
