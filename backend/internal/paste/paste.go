// Package paste defines the Paste domain type shared by the store and API
// layers, and the validation rules applied to incoming paste data.
//
// Data is treated as an opaque, already-encrypted blob produced by the
// client (see frontend/src/crypto) — the server never inspects, logs, or
// otherwise has access to its plaintext contents. The only inspection done
// here is a structural check of the *envelope* around the ciphertext
// (Validate), which keeps the service from being usable as anonymous
// hosting for arbitrary binary content while still learning nothing about
// what the ciphertext decrypts to.
package paste

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrEmpty        = errors.New("paste: data must not be empty")
	ErrTooLarge     = errors.New("paste: data exceeds maximum size")
	ErrMalformed    = errors.New("paste: data is not a valid encrypted envelope")
	ErrInvalidID    = errors.New("paste: invalid paste id")
	ErrInvalidToken = errors.New("paste: invalid read token")
)

// Paste is the domain record for a single stored secret.
type Paste struct {
	ID   string
	Data string // opaque ciphertext envelope, produced client-side

	// ReadToken gates retrieval. The client derives it as
	// base64url(SHA-256(key fragment)) and sends it on every read, so
	// knowing a paste ID alone is not enough to download the ciphertext —
	// or, for a burn-after-read paste, to destroy it. Because it is a hash
	// of the fragment, the server (and anyone who dumps the datastore)
	// still cannot recover the decryption key from it.
	ReadToken string

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

// envelope mirrors the JSON structure frontend/src/crypto/crypto.ts
// produces. Only the framing is checked; ct stays opaque.
type envelope struct {
	V    int    `json:"v"`
	Iter int    `json:"iter"`
	IV   string `json:"iv"`
	CT   string `json:"ct"`
}

const (
	// envelopeVersion is the only blob format this server accepts. Bumping
	// it is a coordinated frontend+backend change.
	envelopeVersion = 1

	// maxIterations bounds the PBKDF2 work factor a paste may declare. The
	// browser that later opens the link is the one that has to run it, so
	// an unbounded value stored here is a denial-of-service against the
	// *recipient*, not this server. The frontend enforces the same cap;
	// this is the copy an attacker calling the API directly cannot skip.
	maxIterations = 2_000_000

	// ivBase64Len is the length of a base64url (unpadded) 12-byte AES-GCM
	// nonce.
	ivBase64Len = 16
)

// Validate checks a paste's opaque data against size limits and the
// expected envelope structure before it is handed to a Store.
// maxSizeBytes must be positive.
func Validate(data string, maxSizeBytes int64) error {
	if len(data) == 0 {
		return ErrEmpty
	}
	if int64(len(data)) > maxSizeBytes {
		return ErrTooLarge
	}

	var env envelope
	dec := json.NewDecoder(strings.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	// Reject anything appended after the envelope, which would otherwise be
	// a free side channel for smuggling arbitrary bytes past this check.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing content after envelope", ErrMalformed)
	}
	if env.V != envelopeVersion {
		return fmt.Errorf("%w: unsupported version %d", ErrMalformed, env.V)
	}
	if env.Iter < 0 || env.Iter > maxIterations {
		return fmt.Errorf("%w: iteration count out of range", ErrMalformed)
	}
	if len(env.IV) != ivBase64Len || !isBase64URL(env.IV) {
		return fmt.Errorf("%w: iv is not a 12-byte base64url nonce", ErrMalformed)
	}
	// An AES-GCM ciphertext is at least the 16-byte tag, so its base64url
	// form is at least 22 characters even for empty plaintext.
	if len(env.CT) < 22 || !isBase64URL(env.CT) {
		return fmt.Errorf("%w: ct is not base64url ciphertext", ErrMalformed)
	}
	return nil
}

func isBase64URL(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
