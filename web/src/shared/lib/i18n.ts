import i18next from "i18next";
import { initReactI18next } from "react-i18next";

import authEN from "@/locales/en/auth.json";
import authRU from "@/locales/ru/auth.json";
import activityEN from "@/locales/en/activity.json";
import activityRU from "@/locales/ru/activity.json";
import enrollmentEN from "@/locales/en/enrollment.json";
import enrollmentRU from "@/locales/ru/enrollment.json";
import enrollmentAttemptsEN from "@/locales/en/enrollment-attempts.json";
import enrollmentAttemptsRU from "@/locales/ru/enrollment-attempts.json";
import runtimeEventsEN from "@/locales/en/runtime-events.json";
import runtimeEventsRU from "@/locales/ru/runtime-events.json";

// Phase-3 §3.2: i18n bootstrap. Russian is the canonical source of
// truth for translation work (the panel was built ru-first), but the
// default for fresh sessions is English: the rest of the panel is
// still hardcoded English literals, and shipping a half-translated
// surface in RU is worse than a consistently-English one until every
// string is i18n'd. Russian remains a fully-supported language and the
// canonical translation reference; operators who pick "ru" in profile
// settings keep their choice via the panvex_lang cookie. Future
// locales add more bundles below; the namespace scheme — one JSON per
// feature folder — keeps lazy-loading viable once we move to
// i18next-http-backend.
//
// Detection strategy: cookie fallback is intentional. Browser
// `navigator.language` is too eager — operators sharing a workstation
// would each see a different language for the same panel. Instead
// the panel sticks to the user's last explicit choice (cookie set
// when the operator picks ru/en in profile settings).
export const SUPPORTED_LANGUAGES = ["ru", "en"] as const;
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number];
export const DEFAULT_LANGUAGE: SupportedLanguage = "en";
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
    ns: ["auth", "activity", "enrollment", "enrollment-attempts", "runtime-events"],
    resources: {
      ru: {
        auth: authRU,
        activity: activityRU,
        enrollment: enrollmentRU,
        "enrollment-attempts": enrollmentAttemptsRU,
        "runtime-events": runtimeEventsRU,
      },
      en: {
        auth: authEN,
        activity: activityEN,
        enrollment: enrollmentEN,
        "enrollment-attempts": enrollmentAttemptsEN,
        "runtime-events": runtimeEventsEN,
      },
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
