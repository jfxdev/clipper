package paste

import (
	"crypto/rand"
	"encoding/base64"
)

// idBytes is the amount of randomness backing each paste ID: 128 bits,
// enough that IDs are not practically guessable/enumerable.
const idBytes = 16

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
