// Package paste defines the Paste domain type shared by the store and API
// layers, and the validation rules applied to incoming paste data.
//
// Data is treated as an opaque, already-encrypted blob produced by the
// client (see frontend/src/crypto) — the server never inspects, logs, or
// otherwise has access to its plaintext contents.
package paste

import (
	"errors"
	"time"
)

var (
	ErrEmpty    = errors.New("paste: data must not be empty")
	ErrTooLarge = errors.New("paste: data exceeds maximum size")
)

// Paste is the domain record for a single stored secret.
type Paste struct {
	ID                string
	Data              string    // opaque ciphertext blob, produced client-side
	ExpiresAt         time.Time // zero value means "never expires"
	BurnAfterRead     bool
	PasswordProtected bool
	CreatedAt         time.Time
	SizeBytes         int
}

// IsExpired reports whether p's ExpiresAt has passed as of now. A zero
// ExpiresAt means the paste never expires. Every Store implementation uses
// this same check as a defensive re-verification on Get, independent of
// whichever backend-native TTL mechanism is also in play.
func (p Paste) IsExpired(now time.Time) bool {
	return !p.ExpiresAt.IsZero() && now.After(p.ExpiresAt)
}

// Validate checks a paste's opaque data against size limits before it is
// handed to a Store. maxSizeBytes must be positive.
func Validate(data string, maxSizeBytes int64) error {
	if len(data) == 0 {
		return ErrEmpty
	}
	if int64(len(data)) > maxSizeBytes {
		return ErrTooLarge
	}
	return nil
}
