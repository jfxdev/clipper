// Base64url and UTF-8 helpers used throughout the crypto module. Kept
// separate from crypto.ts/kdf.ts so those files read as pure crypto logic.
//
// Everything here is explicitly typed Uint8Array<ArrayBuffer> (never the
// wider Uint8Array<ArrayBufferLike>) because the Web Crypto APIs
// (SubtleCrypto.encrypt/decrypt/importKey/deriveKey) require a concrete
// ArrayBuffer-backed BufferSource, not a SharedArrayBuffer-backed one.

const BASE64URL_PATTERN = /^[A-Za-z0-9_-]*$/

export function bytesToBase64Url(bytes: Uint8Array<ArrayBuffer>): string {
  let binary = ""
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

export function base64UrlToBytes(value: string): Uint8Array<ArrayBuffer> {
  // Everything decoded here came off the wire from a server that could be
  // hostile or simply wrong, so the alphabet is checked before atob rather
  // than relying on atob's own (looser, standard-alphabet) parsing.
  if (!BASE64URL_PATTERN.test(value)) {
    throw new Error("value is not base64url")
  }
  let base64 = value.replace(/-/g, "+").replace(/_/g, "/")
  while (base64.length % 4 !== 0) base64 += "="
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes
}

export function textToBytes(text: string): Uint8Array<ArrayBuffer> {
  // Re-wrapped in a fresh Uint8Array so the result is guaranteed
  // ArrayBuffer-backed, matching TextEncoder#encode's looser return type.
  return new Uint8Array(new TextEncoder().encode(text))
}

export function bytesToText(bytes: Uint8Array<ArrayBuffer>): string {
  return new TextDecoder().decode(bytes)
}
