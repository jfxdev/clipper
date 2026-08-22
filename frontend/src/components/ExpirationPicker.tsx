import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

const OPTIONS = [
  { seconds: 600, label: "10 minutos" },
  { seconds: 3600, label: "1 hora" },
  { seconds: 86400, label: "1 dia" },
  { seconds: 604800, label: "1 semana" },
  { seconds: 0, label: "Nunca" },
] as const

interface ExpirationPickerProps {
  value: number
  onChange: (seconds: number) => void
}

export function ExpirationPicker({ value, onChange }: ExpirationPickerProps) {
  return (
    <Select value={String(value)} onValueChange={(v) => onChange(Number(v))}>
      <SelectTrigger className="w-full">
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
