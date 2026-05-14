import i18next from "i18next";
import { initReactI18next } from "react-i18next";

// Phase-3 §3.2: i18n bootstrap. Russian is the canonical source of
// truth for translation work (the panel was built ru-first), but the
// default for fresh sessions is English. Russian remains a
// fully-supported language and the canonical translation reference;
// operators who pick "ru" in profile settings keep their choice via
// the panvex_lang cookie.
//
// Detection strategy: cookie fallback is intentional. Browser
// `navigator.language` is too eager — operators sharing a workstation
// would each see a different language for the same panel. Instead
// the panel sticks to the user's last explicit choice (cookie set
// when the operator picks ru/en in profile settings).
//
// Resource loading: each language's bundle is a separate dynamic
// chunk (i18n-resources-{ru,en}.ts) so only the active language's
// JSON ships with the page. This keeps the App-entry size budget
// realistic — eager-importing all 22 namespace JSONs added ~25 KB
// gzipped to the entry chunk, which is bigger than the entry itself.
export const SUPPORTED_LANGUAGES = ["ru", "en"] as const;
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];
export const DEFAULT_LANGUAGE: SupportedLanguage = "en";
export const LANGUAGE_COOKIE = "panvex_lang";

const NAMESPACES = [
  "auth",
  "activity",
  "enrollment",
  "enrollment-attempts",
  "runtime-events",
  "fleet-groups",
  "dashboard",
  "users",
  "settings",
  "clients",
  "servers",
] as const;

function readCookie(name: string): string | undefined {
  if (typeof document === "undefined") return undefined;
  for (const cookie of document.cookie.split(/;\s*/)) {
    const eq = cookie.indexOf("=");
    if (eq > 0 && cookie.slice(0, eq) === name) {
      return decodeURIComponent(cookie.slice(eq + 1));
    }
  }
  return undefined;
}

function detectInitialLanguage(): SupportedLanguage {
  const stored = readCookie(LANGUAGE_COOKIE);
  if (stored && (SUPPORTED_LANGUAGES as readonly string[]).includes(stored)) {
    return stored as SupportedLanguage;
  }
  return DEFAULT_LANGUAGE;
}

async function loadLanguage(lng: SupportedLanguage) {
  const mod =
    lng === "ru"
      ? await import("./i18n-resources-ru")
      : await import("./i18n-resources-en");
  return mod.resources;
}

let initPromise: Promise<typeof i18next> | null = null;
const loadedLanguages = new Set<SupportedLanguage>();

function registerResources(
  lng: SupportedLanguage,
  resources: Awaited<ReturnType<typeof loadLanguage>>,
) {
  for (const [ns, bundle] of Object.entries(resources)) {
    i18next.addResourceBundle(lng, ns, bundle, true, true);
  }
  loadedLanguages.add(lng);
}

export function initI18n(): Promise<typeof i18next> {
  if (initPromise) return initPromise;
  initPromise = (async () => {
    const lng = detectInitialLanguage();
    const resources = await loadLanguage(lng);
    await i18next.use(initReactI18next).init({
      lng,
      fallbackLng: DEFAULT_LANGUAGE,
      supportedLngs: SUPPORTED_LANGUAGES as readonly string[],
      defaultNS: "common",
      ns: NAMESPACES as readonly string[],
      resources: { [lng]: resources },
      interpolation: {
        // React already escapes — letting i18next double-escape would
        // mangle apostrophes and ampersands in user-facing strings.
        escapeValue: false,
      },
      returnNull: false,
    });
    loadedLanguages.add(lng);
    return i18next;
  })();
  return initPromise;
}

function writeLanguageCookie(lng: SupportedLanguage) {
  if (typeof document === "undefined") return;
  // Year-long expiry, root path, SameSite=Lax so the cookie survives
  // OAuth-style redirects back to the panel.
  const oneYear = 60 * 60 * 24 * 365;
  document.cookie =
    `${LANGUAGE_COOKIE}=${encodeURIComponent(lng)}; ` +
    `path=/; max-age=${oneYear}; samesite=lax`;
}

// setLanguage switches the active language at runtime: lazily fetches
// the requested language's chunk on first use, persists the choice in
// the panvex_lang cookie, and triggers i18next/react-i18next to
// re-render every translated string. Components consuming useTranslation
// pick up the change automatically.
export async function setLanguage(lng: SupportedLanguage): Promise<void> {
  if (!loadedLanguages.has(lng)) {
    const resources = await loadLanguage(lng);
    registerResources(lng, resources);
  }
  writeLanguageCookie(lng);
  await i18next.changeLanguage(lng);
}
