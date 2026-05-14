// Vitest global setup (P2-TEST-01).
//
// `@testing-library/jest-dom/vitest` registers matchers like
// `toBeInTheDocument`, `toHaveTextContent`, etc. on the Vitest `expect`
// so every test file gets them without a per-file import.
import "@testing-library/jest-dom/vitest";

// Phase-3 §3.2 follow-up: bootstrap i18next so any test that mounts a
// component using useTranslation() gets translated strings instead of
// raw keys. Production code calls initI18n() from app/main.tsx; tests
// don't render that entry, so we initialise here.
import { initI18n } from "@/shared/lib/i18n";
await initI18n();
