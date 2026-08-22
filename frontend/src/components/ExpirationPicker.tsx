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
  { seconds: 600, label: "10 minutos" },
  { seconds: 3600, label: "1 hora" },
  { seconds: 86400, label: "1 dia" },
  { seconds: 604800, label: "1 semana" },
  { seconds: 2592000, label: "30 dias" },
] as const

interface ExpirationPickerProps {
  id?: string
  value: number
  onChange: (seconds: number) => void
}

export function ExpirationPicker({ id, value, onChange }: ExpirationPickerProps) {
  return (
    <Select value={String(value)} onValueChange={(v) => onChange(Number(v))}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue placeholder="Expiração" />
      </SelectTrigger>
      <SelectContent>
        {OPTIONS.map((opt) => (
          <SelectItem key={opt.seconds} value={String(opt.seconds)}>
            {opt.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
