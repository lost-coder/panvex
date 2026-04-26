// --- Login ---

export interface LoginPageProps {
  /** Called with credentials only on stage 1. If TOTP is required, parent sets stage to "totp". */
  onCredentials: (username: string, password: string) => Promise<void>;
  /** Called on stage 2 with the TOTP code. Parent has already stored username/password. */
  onTotp: (totpCode: string) => Promise<void>;
  /** Called when user clicks "Back" on stage 2 */
  onBack: () => void;
  /** Controls which stage is shown — parent owns this state */
  stage?: "credentials" | "totp" | undefined;
  error?: string | undefined;
  loading?: boolean | undefined;
}
