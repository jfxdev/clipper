import { textToBytes } from "./encoding"

// Current PBKDF2 work factor for new pastes. This is intentionally NOT
// baked into decryption directly — decryptBlob reads the iteration count
// that was stored alongside the paste at encryption time, so raising this
// constant in a future release doesn't break decrypting older pastes.
export const PBKDF2_ITERATIONS = 310_000

/**
 * Derives the actual AES-256-GCM key used to encrypt/decrypt a paste.
 *
 * With no password, the random base key (the one that ends up in the URL
 * fragment) is used directly as the AES key. With a password, the real key
 * is PBKDF2(password, salt=baseKeyBytes, iterations, SHA-256) instead — so
 * the base key alone (e.g. a leaked URL) is not sufficient to decrypt; the
 * password is still required.
 */
export async function deriveKey(
  baseKeyBytes: Uint8Array<ArrayBuffer>,
  password: string | undefined,
  iterations: number
): Promise<CryptoKey> {
  if (!password) {
    return crypto.subtle.importKey("raw", baseKeyBytes, "AES-GCM", false, [
      "encrypt",
      "decrypt",
    ])
  }

  const passwordKey = await crypto.subtle.importKey(
    "raw",
    textToBytes(password),
    "PBKDF2",
    false,
    ["deriveKey"]
  )

  return crypto.subtle.deriveKey(
    {
      name: "PBKDF2",
      salt: baseKeyBytes,
      iterations,
      hash: "SHA-256",
    },
    passwordKey,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"]
  )
}
