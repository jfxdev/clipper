import { Switch } from "@/components/ui/switch"
import { Label } from "@/components/ui/label"

interface BurnAfterReadToggleProps {
  checked: boolean
  onChange: (checked: boolean) => void
}

export function BurnAfterReadToggle({ checked, onChange }: BurnAfterReadToggleProps) {
  return (
    <div className="flex items-center gap-2">
      <Switch id="burn-after-read" checked={checked} onCheckedChange={onChange} />
      <Label htmlFor="burn-after-read">Destruir após a leitura</Label>
    </div>
  )
}
