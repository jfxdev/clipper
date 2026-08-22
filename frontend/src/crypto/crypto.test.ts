import { describe, expect, it } from "vitest"

import { encryptText, decryptBlob } from "./crypto"

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
