import i18n from "i18next"
import { initReactI18next } from "react-i18next"
import LanguageDetector from "i18next-browser-languagedetector"

import ptBR from "./locales/pt-BR.json"
import en from "./locales/en.json"
import es from "./locales/es.json"

export const SUPPORTED_LANGUAGES = ["pt", "en", "es"] as const
export type SupportedLanguage = (typeof SUPPORTED_LANGUAGES)[number]

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      pt: { translation: ptBR },
      en: { translation: en },
      es: { translation: es },
    },
    fallbackLng: "en",
    supportedLngs: SUPPORTED_LANGUAGES,
    // Strips the region (pt-PT, es-MX, ...) down to the base language, so
    // any variant of a supported language lands on the one translation we
    // actually ship for it — pt-BR is the only Portuguese this app has.
    load: "languageOnly",
    detection: {
      order: ["localStorage", "navigator"],
      caches: ["localStorage"],
      lookupLocalStorage: "clipper:lang",
    },
    interpolation: {
      // React already escapes output, so double-escaping here would just
      // mangle characters like "—" that appear in the translated strings.
      escapeValue: false,
    },
  })

// Keeps <html lang> in sync so assistive tech and browser features
// (spellcheck, translate prompts) match what's actually on screen instead
// of the "pt-BR" baked into index.html for the pre-hydration flash.
function syncHtmlLang(lng: string) {
  document.documentElement.lang = lng
}
i18n.on("languageChanged", syncHtmlLang)
if (i18n.language) syncHtmlLang(i18n.language)

export default i18n
