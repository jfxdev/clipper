import { useEffect, useState, type FormEvent } from "react"
import { useParams } from "react-router-dom"
import { AlertCircle, Flame } from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { PasswordField } from "@/components/PasswordField"
import { BurnConfirmDialog } from "@/components/BurnConfirmDialog"
import { getPaste, ApiError, type GetPasteResponse } from "@/api/client"
import { decryptBlob } from "@/crypto/crypto"
import { parseShareHash } from "@/lib/shareLink"

type Phase =
  | { kind: "invalid-link" }
  | { kind: "awaiting-burn-confirm" }
  | { kind: "cancelled" }
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "needs-password"; paste: GetPasteResponse; error?: string }
  | { kind: "ready"; plaintext: string; burnAfterRead: boolean }

// ViewPastePage re-keys ViewPasteInner by the route id, forcing a full
// remount (and thus a full state reset — hashInfo, phase, any decrypted
// plaintext) whenever the user navigates from one paste link to another
// without a full page load. React Router reuses the same component
// instance across param changes on the same route by default, so without
// this a previous paste's plaintext could linger on screen while the new
// id's data never loads.
export function ViewPastePage() {
  const { id } = useParams<{ id: string }>()
  return <ViewPasteInner key={id} id={id} />
}

function ViewPasteInner({ id }: { id?: string }) {
  const [hashInfo] = useState(() => parseShareHash(window.location.hash))
  const [phase, setPhase] = useState<Phase>(() => {
    if (!id || !hashInfo) return { kind: "invalid-link" }
    return hashInfo.isBurnHint ? { kind: "awaiting-burn-confirm" } : { kind: "loading" }
  })

  async function load(password?: string) {
    if (!id || !hashInfo) return
    setPhase({ kind: "loading" })
    try {
      const paste = await getPaste(id)
      if (paste.passwordProtected && password === undefined) {
        setPhase({ kind: "needs-password", paste })
        return
      }
      const plaintext = await decryptBlob(paste.data, hashInfo.keyFragment, password)
      setPhase({ kind: "ready", plaintext, burnAfterRead: paste.burnAfterRead })
    } catch (err) {
      setPhase({
        kind: "error",
        message:
          err instanceof ApiError
            ? err.status === 404
              ? "Este link não existe, já expirou ou já foi lido."
              : err.message
            : "Não foi possível decifrar esta mensagem. O link pode estar incompleto.",
      })
    }
  }

  // Non-burn pastes fetch immediately; burn-after-read pastes wait for the
  // BurnConfirmDialog so simply opening the page can't destroy the message.
  useEffect(() => {
    if (hashInfo && !hashInfo.isBurnHint) {
      void load()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function handlePasswordSubmit(paste: GetPasteResponse, password: string) {
    if (!hashInfo) return
    try {
      const plaintext = await decryptBlob(paste.data, hashInfo.keyFragment, password)
      setPhase({ kind: "ready", plaintext, burnAfterRead: paste.burnAfterRead })
    } catch {
      setPhase({ kind: "needs-password", paste, error: "Senha incorreta. Tente novamente." })
    }
  }

  if (phase.kind === "invalid-link") {
    return (
      <ErrorCard message='Este link está incompleto: falta a chave de decriptação (o trecho depois de "#").' />
    )
  }

  if (phase.kind === "cancelled") {
    return <ErrorCard message="Leitura cancelada. A mensagem não foi acessada." />
  }

  if (phase.kind === "awaiting-burn-confirm") {
    return (
      <>
        <p className="text-muted-foreground text-sm">Aguardando confirmação...</p>
        <BurnConfirmDialog
          open
          onConfirm={() => void load()}
          onCancel={() => setPhase({ kind: "cancelled" })}
        />
      </>
    )
  }

  if (phase.kind === "loading") {
    return <p className="text-muted-foreground text-sm">Carregando...</p>
  }

  if (phase.kind === "error") {
    return <ErrorCard message={phase.message} />
  }

  if (phase.kind === "needs-password") {
    return (
      <PasswordPrompt
        error={phase.error}
        onSubmit={(password) => handlePasswordSubmit(phase.paste, password)}
      />
    )
  }

  return (
    <Card className="w-full max-w-lg">
      <CardHeader>
        <CardTitle>Mensagem</CardTitle>
        {phase.burnAfterRead && (
          <CardDescription className="flex items-center gap-1.5 text-amber-600 dark:text-amber-500">
            <Flame className="size-4" />
            Esta mensagem foi destruída e não pode ser lida novamente.
          </CardDescription>
        )}
      </CardHeader>
      <CardContent>
        <pre className="bg-muted overflow-x-auto rounded-md p-4 text-left text-sm whitespace-pre-wrap break-words">
          {phase.plaintext}
        </pre>
      </CardContent>
    </Card>
  )
}

function ErrorCard({ message }: { message: string }) {
  return (
    <Alert variant="destructive" className="w-full max-w-lg">
      <AlertCircle />
      <AlertTitle>Não foi possível abrir este link</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  )
}

function PasswordPrompt({
  error,
  onSubmit,
}: {
  error?: string
  onSubmit: (password: string) => void
}) {
  const [password, setPassword] = useState("")

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    onSubmit(password)
  }

  return (
    <Card className="w-full max-w-lg">
      <CardHeader>
        <CardTitle>Esta mensagem tem uma senha</CardTitle>
        <CardDescription>Digite a senha combinada com quem te enviou o link.</CardDescription>
      </CardHeader>
      <form onSubmit={handleSubmit}>
        <CardContent className="grid gap-4">
          <PasswordField
            id="view-password"
            label="Senha"
            value={password}
            onChange={setPassword}
            autoFocus
          />
          {error && (
            <Alert variant="destructive">
              <AlertCircle />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
        </CardContent>
        <CardFooter>
          <Button type="submit" className="w-full">
            Decifrar
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
