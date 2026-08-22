// The entire zero-knowledge guarantee lives in this module: the server only
// ever receives what encryptText produces (an opaque blob string) and never
// sees the plaintext, the base key, or any password. Everything here runs
// client-side via the browser's native SubtleCrypto.
import { deriveKey, PBKDF2_ITERATIONS } from "./kdf"
import {
  bytesToBase64Url,
  base64UrlToBytes,
  textToBytes,
  bytesToText,
} from "./encoding"

const BASE_KEY_BYTES = 32 // 256-bit random key material
const IV_BYTES = 12 // AES-GCM standard nonce size
const BLOB_VERSION = 1

// Upper bound accepted for a blob's embedded PBKDF2 iteration count. Well
// above our own PBKDF2_ITERATIONS (310_000) to allow raising it later, but
// far below a count that could meaningfully freeze a browser tab. The
// server stores Data as an opaque blob and never validates it, so anyone
// can create a paste (via a direct API call) with an arbitrary `iter` —
// without this cap, a victim entering any password on such a link would
// have their browser hang computing PBKDF2 with an attacker-chosen
// iteration count.
const MAX_PBKDF2_ITERATIONS = 2_000_000

/** The JSON shape actually stored/transmitted as Paste.Data. */
interface EncryptedBlob {
  v: number
  /** PBKDF2 iteration count used, or 0 if no password was applied. */
  iter: number
  iv: string
  ct: string
}

export interface EncryptResult {
  /** Opaque string to send to the server as the paste's data. */
  blob: string
  /** Base64url base key, goes in the URL fragment — never sent to the server. */
  keyFragment: string
}

export async function encryptText(
  plaintext: string,
  password?: string
): Promise<EncryptResult> {
  const baseKeyBytes = crypto.getRandomValues(new Uint8Array(BASE_KEY_BYTES))
  const iv = crypto.getRandomValues(new Uint8Array(IV_BYTES))
  const iterations = password ? PBKDF2_ITERATIONS : 0

  const key = await deriveKey(baseKeyBytes, password, iterations)
  const ciphertext = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv },
    key,
    textToBytes(plaintext)
  )

  const encoded: EncryptedBlob = {
    v: BLOB_VERSION,
    iter: iterations,
    iv: bytesToBase64Url(iv),
    ct: bytesToBase64Url(new Uint8Array(ciphertext)),
  }

  return {
    blob: JSON.stringify(encoded),
    keyFragment: bytesToBase64Url(baseKeyBytes),
  }
}

export async function decryptBlob(
  blob: string,
  keyFragment: string,
  password?: string
): Promise<string> {
  const parsed = JSON.parse(blob) as EncryptedBlob
  if (parsed.v !== BLOB_VERSION) {
    throw new Error("unsupported paste format version")
  }
  if (
    !Number.isInteger(parsed.iter) ||
    parsed.iter < 0 ||
    parsed.iter > MAX_PBKDF2_ITERATIONS
  ) {
    throw new Error("paste has an invalid PBKDF2 iteration count")
  }

  const baseKeyBytes = base64UrlToBytes(keyFragment)
  const iv = base64UrlToBytes(parsed.iv)
  const ciphertext = base64UrlToBytes(parsed.ct)

  const key = await deriveKey(baseKeyBytes, password, parsed.iter)
  const plaintextBytes = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv },
    key,
    ciphertext
  )
  return bytesToText(new Uint8Array(plaintextBytes))
}
