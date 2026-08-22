// Builds/parses the URL fragment for a paste share link.
//
// The fragment always carries the base decryption key (never sent to the
// server). For burn-after-read pastes it also carries a "burn:" hint prefix
// so ViewPastePage can show a confirmation dialog *before* firing the GET
// that would otherwise destroy the paste on page load. This hint is not
// secret — knowing a link is burn-once carries no confidentiality
// implication — it only ever affects local UX; the server's atomic
// get-and-delete on Store.Get is the actual security boundary regardless of
// whether this hint is present or accurate.
const BURN_HINT_PREFIX = "burn:"

export function buildShareUrl(id: string, keyFragment: string, burnAfterRead: boolean): string {
  const fragment = burnAfterRead ? BURN_HINT_PREFIX + keyFragment : keyFragment
  return `${window.location.origin}/paste/${id}#${fragment}`
}

export interface ParsedShareHash {
  keyFragment: string
  isBurnHint: boolean
}

export function parseShareHash(hash: string): ParsedShareHash | null {
  const raw = hash.startsWith("#") ? hash.slice(1) : hash
  if (!raw) return null
  if (raw.startsWith(BURN_HINT_PREFIX)) {
    const keyFragment = raw.slice(BURN_HINT_PREFIX.length)
    if (!keyFragment) return null
    return { keyFragment, isBurnHint: true }
  }
  return { keyFragment: raw, isBurnHint: false }
}
