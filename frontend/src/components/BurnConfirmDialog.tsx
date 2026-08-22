import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"

interface BurnConfirmDialogProps {
  open: boolean
  onConfirm: () => void
  onCancel: () => void
}

// Gates the burn-after-read fetch behind an explicit confirmation, so
// simply opening/refreshing the page can't destroy the message on its own.
export function BurnConfirmDialog({ open, onConfirm, onCancel }: BurnConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>Esta mensagem será destruída após a leitura</DialogTitle>
          <DialogDescription>
            Assim que você abrir este link, o conteúdo é apagado permanentemente
            do servidor e não poderá ser acessado de novo — nem por você.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            Cancelar
          </Button>
          <Button onClick={onConfirm}>Ver mensagem</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
