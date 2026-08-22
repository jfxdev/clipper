package paste

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

// idBytes is the amount of randomness backing each paste ID: 128 bits,
// enough that IDs are not practically guessable/enumerable.
const idBytes = 16

// idLen is the length of the base64url (unpadded) encoding of idBytes.
const idLen = 22

// readTokenLen is the length of the base64url (unpadded) encoding of a
// SHA-256 digest, which is what the client sends as a read token.
const readTokenLen = 43

// NewID generates a random, URL-safe paste identifier. It is not a secret —
// it only correlates a share link to a stored ciphertext blob — but must be
// unguessable so pastes can't be enumerated.
func NewID() (string, error) {
	b := make([]byte, idBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidateID rejects anything that is not a well-formed ID produced by
// NewID. Every ID reaching a Store is used verbatim as a backend key (Redis
// key suffix, Mongo _id, DynamoDB partition key), so constraining the
// alphabet and length here keeps attacker-controlled strings out of those
// keyspaces and out of log lines.
func ValidateID(id string) error {
	if len(id) != idLen || !isBase64URL(id) {
		return ErrInvalidID
	}
	return nil
}

// ValidateReadToken rejects anything that is not a base64url SHA-256
// digest, which is the only shape the client ever produces.
func ValidateReadToken(token string) error {
	if len(token) != readTokenLen || !isBase64URL(token) {
		return ErrInvalidToken
	}
	return nil
}

// TokensMatch compares two read tokens without leaking their contents
// through timing. Stores that can only filter server-side (Redis Lua, a
// Mongo query predicate) do their own non-constant-time comparison; this is
// used wherever the comparison happens in Go.
func TokensMatch(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
