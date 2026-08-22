// The entire zero-knowledge guarantee lives in this module: the server only
// ever receives what encryptText produces (an opaque blob string) and never
// sees the plaintext, the base key, or any password. Everything here runs
// client-side via the browser's native SubtleCrypto.
import { deriveKey, PBKDF2_ITERATIONS } from "./kdf"
import { pad, unpad } from "./padding"
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
// above our own PBKDF2_ITERATIONS to allow raising it later, but far below
// a count that could meaningfully freeze a browser tab. The server enforces
// the same ceiling; this copy is what protects a recipient whose paste was
// written by a client that bypassed the API entirely.
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
  /**
   * base64url(SHA-256(keyFragment)), sent to the server and required on
   * every read. Derived from the fragment, so it proves the caller holds
   * the full link without revealing the key it is derived from.
   */
  readToken: string
}

/**
 * Derives the read token the server stores and later demands.
 *
 * This is what makes the paste ID safe to leak: an ID that shows up in a
 * proxy log or a chat preview grants nothing on its own, and — crucially —
 * cannot be used to trigger the burn-after-read delete on a message the
 * holder cannot even read.
 *
 * SHA-256 is the right primitive here rather than a slow KDF: the input is
 * 256 bits of uniform randomness, so there is no guessing attack to slow
 * down, and the recipient's browser should not have to grind before it can
 * fetch.
 */
export async function deriveReadToken(keyFragment: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", textToBytes(keyFragment))
  return bytesToBase64Url(new Uint8Array(digest))
}

/**
 * The authenticated-but-unencrypted header. Binding it in as AES-GCM
 * additional data means the version and iteration count cannot be altered
 * in transit or at rest without the tag check failing — otherwise a hostile
 * server could rewrite `iter` (or downgrade `v` once a v2 exists) and the
 * recipient would have no way to notice.
 */
function headerBytes(v: number, iter: number): Uint8Array<ArrayBuffer> {
  return textToBytes(JSON.stringify({ v, iter }))
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
    {
      name: "AES-GCM",
      iv,
      additionalData: headerBytes(BLOB_VERSION, iterations),
    },
    key,
    pad(textToBytes(plaintext))
  )

  const encoded: EncryptedBlob = {
    v: BLOB_VERSION,
    iter: iterations,
    iv: bytesToBase64Url(iv),
    ct: bytesToBase64Url(new Uint8Array(ciphertext)),
  }

  const keyFragment = bytesToBase64Url(baseKeyBytes)
  return {
    blob: JSON.stringify(encoded),
    keyFragment,
    readToken: await deriveReadToken(keyFragment),
  }
}

/**
 * Parses and structurally validates a blob received from the server. The
 * server is not trusted to have stored back what it was given, so every
 * field is checked for type and range before any of it reaches SubtleCrypto.
 */
function parseBlob(blob: string): EncryptedBlob {
  let parsed: unknown
  try {
    parsed = JSON.parse(blob)
  } catch {
    throw new Error("paste data is not valid JSON")
  }
  if (typeof parsed !== "object" || parsed === null) {
    throw new Error("paste data is not an object")
  }
  const { v, iter, iv, ct } = parsed as Record<string, unknown>
  if (v !== BLOB_VERSION) {
    throw new Error("unsupported paste format version")
  }
  if (
    typeof iter !== "number" ||
    !Number.isInteger(iter) ||
    iter < 0 ||
    iter > MAX_PBKDF2_ITERATIONS
  ) {
    throw new Error("paste has an invalid PBKDF2 iteration count")
  }
  if (typeof iv !== "string" || typeof ct !== "string") {
    throw new Error("paste is missing its iv or ciphertext")
  }
  return { v, iter, iv, ct }
}

export async function decryptBlob(
  blob: string,
  keyFragment: string,
  password?: string
): Promise<string> {
  const parsed = parseBlob(blob)

  const baseKeyBytes = base64UrlToBytes(keyFragment)
  const iv = base64UrlToBytes(parsed.iv)
  const ciphertext = base64UrlToBytes(parsed.ct)

  const key = await deriveKey(baseKeyBytes, password, parsed.iter)
  const plaintextBytes = await crypto.subtle.decrypt(
    {
      name: "AES-GCM",
      iv,
      additionalData: headerBytes(parsed.v, parsed.iter),
    },
    key,
    ciphertext
  )
  return bytesToText(unpad(new Uint8Array(plaintextBytes)))
}
