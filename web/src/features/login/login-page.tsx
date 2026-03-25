import { useState } from "react";
import { useRouter } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api";

export function LoginPage() {
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [showTotp, setShowTotp] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await apiClient.login({ username, password, totp_code: totp || undefined });
      router.navigate({ to: "/" });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes("totp") || msg.includes("TOTP")) {
        setShowTotp(true);
        setError("Enter your TOTP code.");
      } else {
        setError(msg || "Login failed.");
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center">
      <div className="w-full max-w-sm bg-card border border-border rounded p-8 shadow-card-hover backdrop-blur-[var(--blur)]">
        <div className="mb-6 text-center">
          <span className="inline-flex items-center gap-2">
            <span className="w-8 h-8 rounded bg-accent flex items-center justify-center text-white font-extrabold text-sm">P</span>
            <span className="text-xl font-extrabold text-text-1">Panvex</span>
          </span>
        </div>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-xs font-semibold uppercase tracking-[0.1em] text-text-3 mb-1.5">Username</label>
            <Input value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" required />
          </div>
          <div>
            <label className="block text-xs font-semibold uppercase tracking-[0.1em] text-text-3 mb-1.5">Password</label>
            <Input type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="current-password" required />
          </div>
          {showTotp && (
            <div>
              <label className="block text-xs font-semibold uppercase tracking-[0.1em] text-text-3 mb-1.5">TOTP Code</label>
              <Input value={totp} onChange={e => setTotp(e.target.value)} maxLength={6} placeholder="000000" />
            </div>
          )}
          {error && (
            <div className="bg-bad-dim text-bad-text rounded-xs px-3 py-2 text-xs">{error}</div>
          )}
          <Button type="submit" className="w-full mt-4" disabled={loading}>
            {loading ? "Logging in..." : "Log in"}
          </Button>
        </form>
      </div>
    </div>
  );
}
