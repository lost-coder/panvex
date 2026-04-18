// P3-FE-01: recomposed locally from UI-kit primitives (Button, Input) instead
// of importing the pre-built page from @lost-coder/panvex-ui/pages. The UI-kit
// only ships primitives/components/compositions — page composition lives here.
import * as React from "react";
import { Button, Input, type LoginPageProps } from "@lost-coder/panvex-ui";

// ─── Stage panels ─────────────────────────────────────────────────────────────

function CredentialsPanel({
  username,
  password,
  onUsernameChange,
  onPasswordChange,
  onSubmit,
  loading,
}: {
  username: string;
  password: string;
  onUsernameChange: (v: string) => void;
  onPasswordChange: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void;
  loading?: boolean;
}) {
  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-fg-muted uppercase tracking-wider">
          Username
        </label>
        <Input
          type="text"
          autoComplete="username"
          placeholder="admin"
          value={username}
          onChange={(e) => onUsernameChange(e.target.value)}
          disabled={loading}
          required
          autoFocus
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-fg-muted uppercase tracking-wider">
          Password
        </label>
        <Input
          type="password"
          autoComplete="current-password"
          placeholder="••••••••"
          value={password}
          onChange={(e) => onPasswordChange(e.target.value)}
          disabled={loading}
          required
        />
      </div>

      <Button type="submit" className="w-full mt-2" disabled={loading || !username || !password}>
        {loading ? "Signing in…" : "Sign in"}
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
}: {
  totpCode: string;
  onTotpChange: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void;
  onBack: () => void;
  loading?: boolean;
}) {
  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-fg-muted uppercase tracking-wider">
          2FA Code
        </label>
        <Input
          type="text"
          autoComplete="one-time-code"
          inputMode="numeric"
          maxLength={6}
          placeholder="000000"
          value={totpCode}
          onChange={(e) => onTotpChange(e.target.value)}
          disabled={loading}
          required
          autoFocus
        />
        <p className="text-xs text-fg-muted">Enter the 6-digit code from your authenticator app.</p>
      </div>

      <Button type="submit" className="w-full mt-2" disabled={loading || totpCode.length < 6}>
        {loading ? "Verifying…" : "Verify"}
      </Button>

      <button
        type="button"
        onClick={onBack}
        disabled={loading}
        className="text-xs text-fg-muted hover:text-fg self-center transition-colors disabled:opacity-50"
      >
        ← Back
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
}: LoginPageProps) {
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
    <div className="min-h-screen flex items-center justify-center bg-bg">
      <div className="w-full max-w-sm bg-bg-card border border-border rounded-xl shadow-xl p-8 flex flex-col gap-6">
        {/* Logo / Title */}
        <div className="flex flex-col items-center gap-1">
          <span className="font-mono text-3xl font-bold text-fg tracking-tight">Panvex</span>
          <span className="text-xs text-fg-muted uppercase tracking-widest font-mono">
            Control Plane
          </span>
        </div>

        {/* Stage subtitle */}
        {stage === "totp" && (
          <div className="text-center text-sm text-fg-muted -mb-2">Two-factor authentication</div>
        )}

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
