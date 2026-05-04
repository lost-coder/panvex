import i18next from "i18next";
import { initReactI18next } from "react-i18next";

import authEN from "@/locales/en/auth.json";
import authRU from "@/locales/ru/auth.json";
import activityEN from "@/locales/en/activity.json";
import activityRU from "@/locales/ru/activity.json";

// Phase-3 §3.2: i18n bootstrap. Russian is the canonical source of
// truth (the panel was built ru-first), English is the second
// language. Future locales add more bundles below; the namespace
// scheme — one JSON per feature folder — keeps lazy-loading viable
// once we move to i18next-http-backend.
//
// Detection strategy: cookie fallback is intentional. Browser
// `navigator.language` is too eager — operators sharing a workstation
// would each see a different language for the same panel. Instead
// the panel sticks to the user's last explicit choice (cookie set
// when the operator picks ru/en in profile settings).
export const SUPPORTED_LANGUAGES = ["ru", "en"] as const;
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];
export const DEFAULT_LANGUAGE: SupportedLanguage = "ru";
export const LANGUAGE_COOKIE = "panvex_lang";

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

let initialised = false;

export function initI18n(): typeof i18next {
  if (initialised) return i18next;
  initialised = true;

  void i18next.use(initReactI18next).init({
    lng: detectInitialLanguage(),
    fallbackLng: DEFAULT_LANGUAGE,
    supportedLngs: SUPPORTED_LANGUAGES as readonly string[],
    defaultNS: "common",
    ns: ["auth", "activity"],
    resources: {
      ru: { auth: authRU, activity: activityRU },
      en: { auth: authEN, activity: activityEN },
    },
    interpolation: {
      // React already escapes — letting i18next double-escape would
      // mangle apostrophes and ampersands in user-facing strings.
      escapeValue: false,
    },
    returnNull: false,
  });

  return i18next;
}
