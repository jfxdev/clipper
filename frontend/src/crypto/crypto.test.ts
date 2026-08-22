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
})
