import { useTranslation } from "react-i18next"

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

// Every option is a real expiry: a secret that never expires outlives the
// reason it was shared, and the server rejects "never" outright. The
// longest option matches the backend's default MAX_EXPIRE_SECONDS.
const OPTIONS = [
  { seconds: 600, key: "10m" },
  { seconds: 3600, key: "1h" },
  { seconds: 86400, key: "1d" },
  { seconds: 604800, key: "1w" },
  { seconds: 2592000, key: "30d" },
] as const

interface ExpirationPickerProps {
  id?: string
  value: number
  onChange: (seconds: number) => void
}

export function ExpirationPicker({ id, value, onChange }: ExpirationPickerProps) {
  const { t } = useTranslation()
  return (
    <Select value={String(value)} onValueChange={(v) => onChange(Number(v))}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue placeholder={t("expiration.placeholder")} />
      </SelectTrigger>
      <SelectContent>
        {OPTIONS.map((opt) => (
          <SelectItem key={opt.seconds} value={String(opt.seconds)}>
            {t(`expiration.${opt.key}`)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
