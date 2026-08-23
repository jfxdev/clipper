import { useTranslation } from "react-i18next"

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
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onCancel() }}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t("burnConfirm.title")}</DialogTitle>
          <DialogDescription>{t("burnConfirm.description")}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button onClick={onConfirm}>{t("burnConfirm.confirm")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
