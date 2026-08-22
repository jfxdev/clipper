import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

interface PasswordFieldProps {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  autoFocus?: boolean
  hint?: string
}

export function PasswordField({
  id,
  label,
  value,
  onChange,
  placeholder,
  autoFocus,
  hint,
}: PasswordFieldProps) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="password"
        autoComplete="off"
        autoFocus={autoFocus}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        spellCheck={false}
        autoCorrect="off"
        autoCapitalize="off"
      />
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  )
}
