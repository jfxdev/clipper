import { useTranslation } from "react-i18next"

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { SUPPORTED_LANGUAGES, type SupportedLanguage } from "@/i18n"

const LANGUAGE_NAMES: Record<SupportedLanguage, string> = {
  pt: "Português",
  en: "English",
  es: "Español",
}

export function LanguageSwitcher() {
  const { i18n } = useTranslation()
  // i18next-browser-languagedetector can resolve to a region-qualified tag
  // (e.g. "pt-BR") even with load: "languageOnly" applied to resources;
  // normalize back to the base code so the Select's value matches an item.
  const current = i18n.language.split("-")[0]

  return (
    <Select
      value={current}
      onValueChange={(lng) => void i18n.changeLanguage(lng)}
    >
      <SelectTrigger className="w-auto" aria-label="Language">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {SUPPORTED_LANGUAGES.map((lng) => (
          <SelectItem key={lng} value={lng}>
            {LANGUAGE_NAMES[lng]}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
