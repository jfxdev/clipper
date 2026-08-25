import { useEffect, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"
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
import { createPaste, getClientConfig, ApiError } from "@/api/client"
import { buildShareUrl } from "@/lib/shareLink"

const DEFAULT_EXPIRE_SECONDS = 86400 // 1 day

// The ciphertext is downloadable by anyone holding the link, so guessing
// the extra password is an offline attack: there is no server-side lockout
// to slow it down, only the PBKDF2 work factor. A short password buys very
// little against that, so a floor is enforced rather than suggested.
const MIN_PASSWORD_LENGTH = 10

export function CreatePastePage() {
  const { t } = useTranslation()
  const [text, setText] = useState("")
  const [expireSeconds, setExpireSeconds] = useState(DEFAULT_EXPIRE_SECONDS)
  const [burnAfterRead, setBurnAfterRead] = useState(false)
  const [password, setPassword] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [shareUrl, setShareUrl] = useState<string | null>(null)
  // null while the instance capabilities are still loading. On a read-only
  // instance (MODE=read) the create form is replaced by a notice instead of
  // letting the user fill it in only to have the submit rejected.
  const [createEnabled, setCreateEnabled] = useState<boolean | null>(null)

  useEffect(() => {
    let cancelled = false
    getClientConfig()
      .then((cfg) => {
        if (!cancelled) setCreateEnabled(cfg.createEnabled)
      })
      // If the probe itself fails, assume creation is available and let the
      // submit path surface any real error rather than hiding the form.
      .catch(() => {
        if (!cancelled) setCreateEnabled(true)
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!text.trim()) {
      setError(t("create.emptyTextError"))
      return
    }
    if (password && password.length < MIN_PASSWORD_LENGTH) {
      setError(t("create.passwordTooShortError", { count: MIN_PASSWORD_LENGTH }))
      return
    }

    setSubmitting(true)
    setError(null)
    try {
      // Encrypt entirely client-side before anything is sent to the server.
      const { blob, keyFragment, readToken } = await encryptText(text, password || undefined)
      const { id } = await createPaste({
        data: blob,
        expireSeconds,
        burnAfterRead,
        passwordProtected: password.length > 0,
        readToken,
      })
      setShareUrl(buildShareUrl(id, keyFragment, burnAfterRead))
      // Drop the plaintext and password from state as soon as we no longer
      // need them.
      setText("")
      setPassword("")
    } catch (err) {
      if (err instanceof ApiError) {
        // 403 here means this instance is read-only (MODE=read). The server
        // sends a generic body; the mode-specific wording lives in i18n.
        setError(err.status === 403 ? t("create.readOnlyInstance") : err.message)
      } else {
        setError(t("create.genericError"))
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (shareUrl) {
    return (
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>{t("create.readyTitle")}</CardTitle>
          <CardDescription>{t("create.readyDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <ShareLinkBox url={shareUrl} />
        </CardContent>
        <CardFooter>
          <Button variant="outline" onClick={() => setShareUrl(null)}>
            {t("create.createAnother")}
          </Button>
        </CardFooter>
      </Card>
    )
  }

  if (createEnabled === null) {
    return <p className="text-muted-foreground text-sm">{t("view.loading")}</p>
  }

  if (!createEnabled) {
    return (
      <Card className="w-full max-w-lg">
        <CardHeader>
          <CardTitle>{t("create.readOnlyTitle")}</CardTitle>
          <CardDescription>{t("create.readOnlyInstance")}</CardDescription>
        </CardHeader>
      </Card>
    )
  }

  return (
    <Card className="w-full max-w-lg">
      <CardHeader>
        <CardTitle>{t("common.appName")}</CardTitle>
        <CardDescription>{t("create.tagline")}</CardDescription>
      </CardHeader>
      <form onSubmit={handleSubmit}>
        <CardContent className="grid gap-4">
          <div className="grid gap-1.5">
            <Label htmlFor="paste-text">{t("create.textLabel")}</Label>
            <Textarea
              id="paste-text"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={t("create.textPlaceholder")}
              rows={8}
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="paste-expiration">{t("create.expirationLabel")}</Label>
            <ExpirationPicker
              id="paste-expiration"
              value={expireSeconds}
              onChange={setExpireSeconds}
            />
          </div>

          <BurnAfterReadToggle checked={burnAfterRead} onChange={setBurnAfterRead} />

          <PasswordField
            id="paste-password"
            label={t("create.passwordLabel")}
            value={password}
            onChange={setPassword}
            placeholder={t("create.passwordPlaceholder")}
            hint={t("create.passwordHint", { count: MIN_PASSWORD_LENGTH })}
          />

          {error && (
            <Alert variant="destructive">
              <AlertCircle />
              <AlertTitle>{t("common.error")}</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
        </CardContent>
        <CardFooter>
          <Button type="submit" disabled={submitting} className="w-full">
            {submitting ? t("create.submitBusy") : t("create.submitIdle")}
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
