import { useTranslation } from "react-i18next"
import { Monitor, Moon, Sun } from "lucide-react"

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useTheme } from "@/components/theme-provider"

const THEME_ICONS = {
  light: Sun,
  dark: Moon,
  system: Monitor,
} as const

export function ThemeSwitcher() {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()
  const Icon = THEME_ICONS[theme]

  return (
    <Select
      value={theme}
      onValueChange={(value) => setTheme(value as typeof theme)}
    >
      <SelectTrigger className="w-auto" aria-label={t("theme.label")}>
        <Icon className="size-4" />
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="light">{t("theme.light")}</SelectItem>
        <SelectItem value="dark">{t("theme.dark")}</SelectItem>
        <SelectItem value="system">{t("theme.system")}</SelectItem>
      </SelectContent>
    </Select>
  )
}
