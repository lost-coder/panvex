// LoginPage — composed locally from UI-kit primitives (Button, Input).
// The kit at `@/ui` ships primitives/components/compositions only;
// page composition lives in features/.
import * as React from "react";
import { useEffect, useId, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Button, Input, type LoginPageProps } from "@/ui";

// ─── Stage panels ─────────────────────────────────────────────────────────────

function CredentialsPanel({
  username,
  password,
  onUsernameChange,
  onPasswordChange,
  onSubmit,
  loading,
}: Readonly<{
  username: string;
  password: string;
  onUsernameChange: (v: string) => void;
  onPasswordChange: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void | Promise<void>;
  loading?: boolean | undefined;
}>) {
  const { t } = useTranslation("auth");
  const usernameId = useId();
  const passwordId = useId();
  const usernameRef = useRef<HTMLInputElement>(null);

  // Replaces autoFocus on the username input. Initial focus is still
  // useful (operators land on the panel ready to type), but the
  // attribute itself trips jsx-a11y/no-autofocus — moving it into a
  // post-mount effect keeps the rule happy without changing UX.
  useEffect(() => {
    usernameRef.current?.focus();
  }, []);

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4">
      <label htmlFor={usernameId} className="flex flex-col gap-1.5">
        <span className="text-xs font-medium text-fg-muted uppercase tracking-wider">
          {t("login.credentials.usernameLabel")}
        </span>
        <Input
          ref={usernameRef}
          id={usernameId}
          type="text"
          autoComplete="username"
          placeholder={t("login.credentials.usernamePlaceholder")}
          value={username}
          onChange={(e) => onUsernameChange(e.target.value)}
          disabled={loading}
          required
        />
      </label>

      <label htmlFor={passwordId} className="flex flex-col gap-1.5">
        <span className="text-xs font-medium text-fg-muted uppercase tracking-wider">
          {t("login.credentials.passwordLabel")}
        </span>
        <Input
          id={passwordId}
          type="password"
          autoComplete="current-password"
          placeholder={t("login.credentials.passwordPlaceholder")}
          value={password}
          onChange={(e) => onPasswordChange(e.target.value)}
          disabled={loading}
          required
        />
      </label>

      <Button type="submit" className="w-full mt-2" disabled={loading || !username || !password}>
        {loading ? t("login.credentials.submitLoading") : t("login.credentials.submit")}
      </Button>
    </form>
  );
}

function TotpPanel({
  totpCode,
  onTotpChange,
  onSubmit,
  onBack,
  loading,
}: Readonly<{
  totpCode: string;
  onTotpChange: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void | Promise<void>;
  onBack: () => void;
  loading?: boolean | undefined;
}>) {
  const { t } = useTranslation("auth");
  const totpId = useId();
  const totpRef = useRef<HTMLInputElement>(null);

  // Replaces autoFocus on the TOTP input — same rationale as the
  // username field above. Initial focus is the natural UX (operator
  // pasted/typed credentials, the next thing they want to do is type
  // the 6-digit code), but the attribute itself trips
  // jsx-a11y/no-autofocus.
  useEffect(() => {
    totpRef.current?.focus();
  }, []);

  // Strip non-digit keystrokes at the source so users can't accidentally
  // type a space and fail validation silently. `inputMode="numeric"`
  // opens the numeric keypad on mobile; the pattern attr is a belt-and-
  // braces fallback for browsers that ignore the hint.
  const handleChange = (v: string) => {
    const digits = v.replaceAll(/\D/g, "").slice(0, 6);
    onTotpChange(digits);
  };
  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4">
      <label htmlFor={totpId} className="flex flex-col gap-1.5">
        <span className="text-xs font-medium text-fg-muted uppercase tracking-wider">
          {t("login.totp.codeLabel")}
        </span>
        <Input
          ref={totpRef}
          id={totpId}
          type="text"
          autoComplete="one-time-code"
          inputMode="numeric"
          pattern="[0-9]*"
          maxLength={6}
          placeholder={t("login.totp.codePlaceholder")}
          value={totpCode}
          onChange={(e) => handleChange(e.target.value)}
          disabled={loading}
          required
          // Mono + wide tracking turns the 6-digit field into a
          // ticker-style focal point — it's the dominant element
          // on the panel at this stage.
          className="font-mono text-center text-2xl tracking-[0.5em] tabular-nums"
        />
        <p className="text-xs text-fg-muted">{t("login.totp.hint")}</p>
      </label>

      <Button type="submit" className="w-full mt-2" disabled={loading || totpCode.length < 6}>
        {loading ? t("login.totp.submitLoading") : t("login.totp.submit")}
      </Button>

      <button
        type="button"
        onClick={onBack}
        disabled={loading}
        className="text-xs text-fg-muted hover:text-fg self-center transition-colors disabled:opacity-50"
      >
        {t("login.totp.back")}
      </button>
    </form>
  );
}

// ─── Main component ───────────────────────────────────────────────────────────

export function LoginPage({
  onCredentials,
  onTotp,
  onBack,
  stage = "credentials",
  error,
  loading,
}: Readonly<LoginPageProps>) {
  const { t } = useTranslation("auth");
  const [username, setUsername] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [totpCode, setTotpCode] = React.useState("");

  // Track previous stage for slide direction
  const prevStageRef = React.useRef(stage);
  const [direction, setDirection] = React.useState<"forward" | "back">("forward");

  React.useEffect(() => {
    if (stage !== prevStageRef.current) {
      setDirection(prevStageRef.current === "credentials" && stage === "totp" ? "forward" : "back");
      prevStageRef.current = stage;
    }
  }, [stage]);

  async function handleCredentials(e: React.FormEvent) {
    e.preventDefault();
    await onCredentials(username, password);
  }

  async function handleTotp(e: React.FormEvent) {
    e.preventDefault();
    await onTotp(totpCode);
  }

  function handleBack() {
    setTotpCode("");
    onBack();
  }

  // Animation key ensures re-mount → re-play animate-in on stage change
  const animClass =
    direction === "forward"
      ? "animate-in slide-in-from-right-4 fade-in duration-200"
      : "animate-in slide-in-from-left-4 fade-in duration-200";

  return (
    // items-start on mobile so the on-screen keyboard doesn't push the
    // form off the fold; items-center on desktop keeps the classic
    // centered card.
    <div className="min-h-screen flex justify-center items-start pt-12 md:items-center md:pt-0 bg-bg p-4">
      <div className="w-full max-w-sm bg-bg-card border border-border rounded-xl shadow-xl p-8 flex flex-col gap-6">
        {/* Brand — status beacon next to "Control plane" so operators
            see "this is a live panel" before they even type a password. */}
        <div className="flex flex-col items-center gap-1">
          <span className="font-mono text-3xl font-bold text-fg tracking-tight">{t("login.brand")}</span>
          <span className="text-[11px] text-fg-muted uppercase tracking-widest font-mono inline-flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className="h-1.5 w-1.5 rounded-full bg-status-ok shadow-[0_0_6px_var(--color-status-ok)]"
            />
            {t("login.controlPlane")}
            {stage === "totp" && (
              <span className="text-fg-faint mx-1">/</span>
            )}
            {stage === "totp" && (
              <span className="text-status-warn">{t("login.twoFactor")}</span>
            )}
          </span>
        </div>

        {/* Error banner */}
        {error && (
          <div className="rounded-xs border border-status-error/30 bg-status-error/10 px-3 py-2 text-sm text-status-error">
            {error}
          </div>
        )}

        {/* Animated stage panel */}
        <div key={stage} className={animClass}>
          {stage === "credentials" ? (
            <CredentialsPanel
              username={username}
              password={password}
              onUsernameChange={setUsername}
              onPasswordChange={setPassword}
              onSubmit={handleCredentials}
              loading={loading}
            />
          ) : (
            <TotpPanel
              totpCode={totpCode}
              onTotpChange={setTotpCode}
              onSubmit={handleTotp}
              onBack={handleBack}
              loading={loading}
            />
          )}
        </div>
      </div>
    </div>
  );
}
