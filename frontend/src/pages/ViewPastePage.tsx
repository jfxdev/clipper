import { useEffect, useState, type FormEvent } from "react"
import { useParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { AlertCircle, Eye, EyeOff, Flame } from "lucide-react"

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
import { decryptBlob, deriveReadToken } from "@/crypto/crypto"
import { parseShareHash } from "@/lib/shareLink"

// clearKeyFragment removes the decryption key from the address bar and from
// the history entry once it has been used. It does not undo the fact that
// the key was in the URL — that is inherent to the design — but it keeps it
// out of a screen-shared address bar, a screenshot, and the entry a later
// visitor to this browser can read out of the history.
function clearKeyFragment() {
  try {
    window.history.replaceState(null, "", window.location.pathname + window.location.search)
  } catch {
    // Some embedded webviews disallow replaceState; nothing here is
    // load-bearing for security, so a failure is not worth surfacing.
  }
}

// clearKeyFragment strips the key from the address bar once a paste is
// decrypted, so a reload of that same tab arrives with no hash — otherwise
// indistinguishable from a link that was never valid. Recording the id here
// lets that reload say "you already read this" instead of "broken link".
const VIEWED_PREFIX = "clipper:viewed:"

function markViewed(id: string) {
  try {
    sessionStorage.setItem(VIEWED_PREFIX + id, "1")
  } catch {
    // Private-browsing modes can disallow sessionStorage; the reload then
    // just falls back to the generic "incomplete link" message.
  }
}

function wasViewed(id: string): boolean {
  try {
    return sessionStorage.getItem(VIEWED_PREFIX + id) === "1"
  } catch {
    return false
  }
}

type Phase =
  | { kind: "invalid-link"; alreadyViewed: boolean }
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
  const { t } = useTranslation()
  const [hashInfo] = useState(() => parseShareHash(window.location.hash))
  const [phase, setPhase] = useState<Phase>(() => {
    if (!id || !hashInfo) {
      return { kind: "invalid-link", alreadyViewed: !!id && wasViewed(id) }
    }
    return hashInfo.isBurnHint ? { kind: "awaiting-burn-confirm" } : { kind: "loading" }
  })

  async function load(password?: string) {
    if (!id || !hashInfo) return
    setPhase({ kind: "loading" })
    try {
      // The read token proves we hold the full link. Without it the server
      // returns 404 — which is what stops anyone who merely saw the paste
      // ID from reading, or burning, this message.
      const readToken = await deriveReadToken(hashInfo.keyFragment)
      const paste = await getPaste(id, readToken)
      if (paste.passwordProtected && password === undefined) {
        setPhase({ kind: "needs-password", paste })
        return
      }
      const plaintext = await decryptBlob(paste.data, hashInfo.keyFragment, password)
      clearKeyFragment()
      markViewed(id)
      setPhase({ kind: "ready", plaintext, burnAfterRead: paste.burnAfterRead })
    } catch (err) {
      setPhase({
        kind: "error",
        message:
          err instanceof ApiError
            ? err.status === 404
              ? t("view.notFound")
              : err.message
            : t("view.decryptError"),
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
    if (!hashInfo || !id) return
    try {
      const plaintext = await decryptBlob(paste.data, hashInfo.keyFragment, password)
      clearKeyFragment()
      markViewed(id)
      setPhase({ kind: "ready", plaintext, burnAfterRead: paste.burnAfterRead })
    } catch {
      setPhase({ kind: "needs-password", paste, error: t("view.passwordPromptWrong") })
    }
  }

  if (phase.kind === "invalid-link") {
    return phase.alreadyViewed ? (
      <ErrorCard message={t("view.alreadyViewed")} />
    ) : (
      <ErrorCard message={t("view.invalidLink")} />
    )
  }

  if (phase.kind === "cancelled") {
    return <ErrorCard message={t("view.cancelledMessage")} />
  }

  if (phase.kind === "awaiting-burn-confirm") {
    return (
      <>
        <p className="text-muted-foreground text-sm">{t("view.awaitingConfirm")}</p>
        <BurnConfirmDialog
          open
          onConfirm={() => void load()}
          onCancel={() => setPhase({ kind: "cancelled" })}
        />
      </>
    )
  }

  if (phase.kind === "loading") {
    return <p className="text-muted-foreground text-sm">{t("view.loading")}</p>
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
        <CardTitle>{t("view.title")}</CardTitle>
        {phase.burnAfterRead && (
          <CardDescription className="flex items-center gap-1.5 text-amber-600 dark:text-amber-500">
            <Flame className="size-4" />
            {t("view.burnedNotice")}
          </CardDescription>
        )}
      </CardHeader>
      <CardContent>
        <PasteReveal text={phase.plaintext} />
      </CardContent>
    </Card>
  )
}

// Hidden by default so the secret isn't shown the instant the page loads —
// over someone's shoulder, in a screen share, or in a browser preview
// thumbnail. The user has to deliberately click to reveal it.
function PasteReveal({ text }: { text: string }) {
  const { t } = useTranslation()
  const [revealed, setRevealed] = useState(false)

  return (
    <div className="relative">
      <pre
        className={`bg-muted overflow-x-auto rounded-md p-4 text-left text-sm whitespace-pre-wrap break-words ${
          revealed ? "" : "select-none blur-sm"
        }`}
      >
        {text}
      </pre>
      {!revealed && (
        <button
          type="button"
          onClick={() => setRevealed(true)}
          className="bg-background/60 hover:bg-background/70 absolute inset-0 flex items-center justify-center gap-1.5 rounded-md text-sm font-medium transition-colors"
        >
          <Eye className="size-4" />
          {t("view.revealButton")}
        </button>
      )}
      {revealed && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="mt-2"
          onClick={() => setRevealed(false)}
        >
          <EyeOff className="size-4" />
          {t("view.hideButton")}
        </Button>
      )}
    </div>
  )
}

function ErrorCard({ message }: { message: string }) {
  const { t } = useTranslation()
  return (
    <Alert variant="destructive" className="w-full max-w-lg">
      <AlertCircle />
      <AlertTitle>{t("view.errorTitle")}</AlertTitle>
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
  const { t } = useTranslation()
  const [password, setPassword] = useState("")

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    onSubmit(password)
  }

  return (
    <Card className="w-full max-w-lg">
      <CardHeader>
        <CardTitle>{t("view.passwordPromptTitle")}</CardTitle>
        <CardDescription>{t("view.passwordPromptDescription")}</CardDescription>
      </CardHeader>
      <form onSubmit={handleSubmit}>
        <CardContent className="grid gap-4">
          <PasswordField
            id="view-password"
            label={t("view.passwordFieldLabel")}
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
            {t("view.decryptButton")}
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
