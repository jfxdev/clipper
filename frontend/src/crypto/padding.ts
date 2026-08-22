// Length-hiding padding.
//
// AES-GCM ciphertext is exactly as long as its plaintext, so without this
// the stored blob size tells anyone with database access — or anyone
// watching response sizes — roughly how long the message is. "6 characters"
// versus "3 kilobytes" is a meaningful distinction when the message might
// be a PIN, a password, or a private key.
//
// Layout: a 4-byte big-endian length prefix, the UTF-8 payload, then zero
// fill up to the next PAD_BLOCK boundary. The whole padded buffer is inside
// the AEAD, so the padding is authenticated along with everything else and
// cannot be manipulated separately.

const PAD_BLOCK = 256
const LENGTH_PREFIX_BYTES = 4

export function pad(payload: Uint8Array<ArrayBuffer>): Uint8Array<ArrayBuffer> {
  const total = LENGTH_PREFIX_BYTES + payload.length
  const paddedLength = Math.ceil(total / PAD_BLOCK) * PAD_BLOCK
  const out = new Uint8Array(paddedLength)
  new DataView(out.buffer).setUint32(0, payload.length, false)
  out.set(payload, LENGTH_PREFIX_BYTES)
  return out
}

export function unpad(padded: Uint8Array<ArrayBuffer>): Uint8Array<ArrayBuffer> {
  if (padded.length < LENGTH_PREFIX_BYTES) {
    throw new Error("padded payload is truncated")
  }
  const length = new DataView(padded.buffer, padded.byteOffset, padded.byteLength).getUint32(0, false)
  if (LENGTH_PREFIX_BYTES + length > padded.length) {
    throw new Error("padded payload declares a length beyond its own size")
  }
  return padded.slice(LENGTH_PREFIX_BYTES, LENGTH_PREFIX_BYTES + length)
}
