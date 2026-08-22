import { useState, type FormEvent } from "react"
import { AlertCircle } from "lucide-react"

import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Textarea } from "@/components/ui/textarea"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { ExpirationPicker } from "@/components/ExpirationPicker"
import { BurnAfterReadToggle } from "@/components/BurnAfterReadToggle"
import { PasswordField } from "@/components/PasswordField"
import { ShareLinkBox } from "@/components/ShareLinkBox"
import { encryptText } from "@/crypto/crypto"
import { createPaste, ApiError } from "@/api/client"
import { buildShareUrl } from "@/lib/shareLink"

const DEFAULT_EXPIRE_SECONDS = 86400 // 1 day

export function CreatePastePage() {
  const [text, setText] = useState("")
  const [expireSeconds, setExpireSeconds] = useState(DEFAULT_EXPIRE_SECONDS)
  const [burnAfterRead, setBurnAfterRead] = useState(false)
  const [password, setPassword] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [shareUrl, setShareUrl] = useState<string | null>(null)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!text.trim()) {
      setError("Digite algum texto para compartilhar.")
      return
    }

    setSubmitting(true)
    setError(null)
    try {
      // Encrypt entirely client-side before anything is sent to the server.
      const { blob, keyFragment } = await encryptText(text, password || undefined)
      const { id } = await createPaste({
        data: blob,
        expireSeconds,
        burnAfterRead,
        passwordProtected: password.length > 0,
      })
      setShareUrl(buildShareUrl(id, keyFragment, burnAfterRead))
      // Drop the plaintext and password from state as soon as we no longer
      // need them.
      setText("")
      setPassword("")
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : "Falha ao criar o link. Tente novamente."
      )
    } finally {
      setSubmitting(false)
    }
  }

  if (shareUrl) {
    return (
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>Link pronto</CardTitle>
          <CardDescription>
            Compartilhe este link com quem deve ler a mensagem. Só quem tiver
            o link completo (incluindo o trecho depois de "#") consegue
            decifrar o conteúdo — nós nunca vimos o texto original.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ShareLinkBox url={shareUrl} />
        </CardContent>
        <CardFooter>
          <Button variant="outline" onClick={() => setShareUrl(null)}>
            Criar outro link
          </Button>
        </CardFooter>
      </Card>
    )
  }

  return (
    <Card className="w-full max-w-lg">
      <CardHeader>
        <CardTitle>clipper</CardTitle>
        <CardDescription>
          Compartilhe texto cifrado que só quem recebe o link consegue ler.
        </CardDescription>
      </CardHeader>
      <form onSubmit={handleSubmit}>
        <CardContent className="grid gap-4">
          <div className="grid gap-1.5">
            <Label htmlFor="paste-text">Texto</Label>
            <Textarea
              id="paste-text"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Cole ou digite o texto a compartilhar..."
              rows={8}
            />
          </div>

          <div className="grid gap-1.5">
            <Label>Expira em</Label>
            <ExpirationPicker value={expireSeconds} onChange={setExpireSeconds} />
          </div>

          <BurnAfterReadToggle checked={burnAfterRead} onChange={setBurnAfterRead} />

          <PasswordField
            id="paste-password"
            label="Senha adicional (opcional)"
            value={password}
            onChange={setPassword}
            placeholder="Deixe em branco para não usar senha"
          />

          {error && (
            <Alert variant="destructive">
              <AlertCircle />
              <AlertTitle>Erro</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
        </CardContent>
        <CardFooter>
          <Button type="submit" disabled={submitting} className="w-full">
            {submitting ? "Cifrando..." : "Criar link cifrado"}
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
