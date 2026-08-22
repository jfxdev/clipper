import { describe, expect, it } from "vitest"

import { encryptText, decryptBlob, deriveReadToken } from "./crypto"

describe("encryptText / decryptBlob", () => {
  it("round-trips plaintext with no password", async () => {
    const plaintext = "a secret message, olá mundo 🔐"
    const { blob, keyFragment } = await encryptText(plaintext)
    const decrypted = await decryptBlob(blob, keyFragment)
    expect(decrypted).toBe(plaintext)
  })

  it("round-trips plaintext with a password", async () => {
    const plaintext = "another secret"
    const { blob, keyFragment } = await encryptText(plaintext, "correct horse battery staple")
    const decrypted = await decryptBlob(blob, keyFragment, "correct horse battery staple")
    expect(decrypted).toBe(plaintext)
  })

  it("fails to decrypt with the wrong password", async () => {
    const { blob, keyFragment } = await encryptText("secret", "right-password")
    await expect(decryptBlob(blob, keyFragment, "wrong-password")).rejects.toThrow()
  })

  it("fails to decrypt a password-protected paste with no password supplied", async () => {
    const { blob, keyFragment } = await encryptText("secret", "right-password")
    await expect(decryptBlob(blob, keyFragment)).rejects.toThrow()
  })

  it("the base key fragment alone is not sufficient when a password was used", async () => {
    // Guards the core security property: leaking the URL fragment (the key
    // fragment) must not be enough to decrypt a password-protected paste.
    const { blob, keyFragment } = await encryptText("top secret", "hunter2")
    await expect(decryptBlob(blob, keyFragment, undefined)).rejects.toThrow()
  })

  it("fails to decrypt when the ciphertext has been tampered with", async () => {
    const { blob, keyFragment } = await encryptText("integrity matters")
    const parsed = JSON.parse(blob) as { ct: string; [k: string]: unknown }

    // Flip one base64url character in the ciphertext to simulate tampering.
    const flipped = parsed.ct[0] === "A" ? "B" : "A"
    parsed.ct = flipped + parsed.ct.slice(1)
    const tamperedBlob = JSON.stringify(parsed)

    await expect(decryptBlob(tamperedBlob, keyFragment)).rejects.toThrow()
  })

  it("produces a different key fragment and ciphertext for every call (no IV/key reuse)", async () => {
    const a = await encryptText("same plaintext")
    const b = await encryptText("same plaintext")
    expect(a.keyFragment).not.toBe(b.keyFragment)
    expect(a.blob).not.toBe(b.blob)
  })

  it("rejects a blob with an oversized PBKDF2 iteration count", async () => {
    // The server never validates the blob it stores, so a malicious paste
    // could carry an absurd iteration count; decryptBlob must reject it
    // before ever calling into PBKDF2, rather than freezing the tab.
    const { blob, keyFragment } = await encryptText("secret", "some-password")
    const parsed = JSON.parse(blob) as { iter: number; [k: string]: unknown }
    parsed.iter = 50_000_000
    const tamperedBlob = JSON.stringify(parsed)

    await expect(decryptBlob(tamperedBlob, keyFragment, "some-password")).rejects.toThrow(
      /iteration count/
    )
  })

  it("rejects a blob with a non-integer PBKDF2 iteration count", async () => {
    const { blob, keyFragment } = await encryptText("secret")
    const parsed = JSON.parse(blob) as { iter: number; [k: string]: unknown }
    parsed.iter = 123.45
    const tamperedBlob = JSON.stringify(parsed)

    await expect(decryptBlob(tamperedBlob, keyFragment)).rejects.toThrow(/iteration count/)
  })
})

describe("blob header authentication", () => {
  it("rejects a blob whose iteration count was altered in transit", async () => {
    // `iter` travels outside the ciphertext, so it is only trustworthy
    // because it is bound in as AES-GCM additional data. A server that
    // rewrites it must not be able to make the recipient derive a
    // different key without the tag check failing.
    const { blob, keyFragment } = await encryptText("secret", "correct horse battery")
    const parsed = JSON.parse(blob) as { iter: number; [k: string]: unknown }
    parsed.iter = 310_000
    await expect(
      decryptBlob(JSON.stringify(parsed), keyFragment, "correct horse battery")
    ).rejects.toThrow()
  })

  it("rejects a blob whose iv was altered", async () => {
    const { blob, keyFragment } = await encryptText("secret")
    const parsed = JSON.parse(blob) as { iv: string; [k: string]: unknown }
    parsed.iv = parsed.iv[0] === "A" ? "B" + parsed.iv.slice(1) : "A" + parsed.iv.slice(1)
    await expect(decryptBlob(JSON.stringify(parsed), keyFragment)).rejects.toThrow()
  })

  it("rejects non-base64url ciphertext instead of mis-decoding it", async () => {
    const { blob, keyFragment } = await encryptText("secret")
    const parsed = JSON.parse(blob) as { ct: string; [k: string]: unknown }
    parsed.ct = "!!!not base64!!!"
    await expect(decryptBlob(JSON.stringify(parsed), keyFragment)).rejects.toThrow(/base64url/)
  })

  it("rejects a blob that is not an object", async () => {
    const { keyFragment } = await encryptText("secret")
    await expect(decryptBlob("42", keyFragment)).rejects.toThrow()
    await expect(decryptBlob("not json at all", keyFragment)).rejects.toThrow()
  })
})

describe("length hiding", () => {
  it("gives short messages of different lengths the same ciphertext size", async () => {
    // Without padding the blob size leaks how long the message is, which
    // for a PIN or a passphrase is most of what an observer wants to know.
    const short = await encryptText("1234")
    const longer = await encryptText("a somewhat longer secret message")
    const size = (blob: string) => (JSON.parse(blob) as { ct: string }).ct.length
    expect(size(short.blob)).toBe(size(longer.blob))
  })

  it("still round-trips payloads that cross a padding boundary", async () => {
    for (const length of [0, 1, 251, 252, 253, 512, 1000]) {
      const plaintext = "x".repeat(length)
      const { blob, keyFragment } = await encryptText(plaintext)
      expect(await decryptBlob(blob, keyFragment)).toBe(plaintext)
    }
  })

  it("round-trips multi-byte characters near a boundary", async () => {
    const plaintext = "🔐".repeat(80) // 4 bytes each, straddles 256 bytes
    const { blob, keyFragment } = await encryptText(plaintext)
    expect(await decryptBlob(blob, keyFragment)).toBe(plaintext)
  })
})

describe("read token", () => {
  it("is a stable base64url SHA-256 digest of the key fragment", async () => {
    const { keyFragment, readToken } = await encryptText("secret")
    expect(readToken).toMatch(/^[A-Za-z0-9_-]{43}$/)
    expect(await deriveReadToken(keyFragment)).toBe(readToken)
  })

  it("differs for every paste, and reveals nothing about the fragment", async () => {
    const a = await encryptText("secret")
    const b = await encryptText("secret")
    expect(a.readToken).not.toBe(b.readToken)
    expect(a.readToken).not.toContain(a.keyFragment)
    expect(a.keyFragment).not.toContain(a.readToken)
  })
})
