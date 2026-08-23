import { useTranslation } from "react-i18next"

import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"

interface BurnAfterReadToggleProps {
  checked: boolean
  onChange: (checked: boolean) => void
}

export function BurnAfterReadToggle({ checked, onChange }: BurnAfterReadToggleProps) {
  const { t } = useTranslation()
  return (
    <div className="flex items-center gap-2">
      <Switch id="burn-after-read" checked={checked} onCheckedChange={onChange} />
      <Label htmlFor="burn-after-read">{t("burnAfterRead.label")}</Label>
    </div>
  )
}
