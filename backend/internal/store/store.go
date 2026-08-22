// Package store defines the Store contract that every backing storage
// implementation (redis, mongo, dynamo, memory) must satisfy.
package store

import (
	"context"
	"errors"

	"github.com/jfxdev/clipper/backend/internal/paste"
)

// ErrNotFound is returned by Get when a paste does not exist, has already
// been burned, or has expired. Implementations must treat "expired but not
// yet physically swept" the same as "not found" — callers never need to
// distinguish the two.
var ErrNotFound = errors.New("store: paste not found")

// Store persists and retrieves opaque, already-encrypted pastes.
//
// Implementations MUST perform the burn-after-read delete atomically as
// part of Get (e.g. Redis GETDEL, Mongo FindOneAndDelete, DynamoDB
// DeleteItem with ReturnValues=ALL_OLD) so two concurrent reads of a
// burn-after-read paste can never both succeed.
type Store interface {
	// Create stores a new paste. Implementations apply their own
	// backend-native TTL mechanism from p.ExpiresAt (a zero value means the
	// paste never expires).
	Create(ctx context.Context, p paste.Paste) error

	// Get retrieves a paste by ID. Returns ErrNotFound if it does not
	// exist or has expired. If the stored paste has BurnAfterRead set, Get
	// atomically deletes it as part of this call before returning it.
	Get(ctx context.Context, id string) (paste.Paste, error)

	// Close releases backend resources (connection pools, etc.) on
	// shutdown.
	Close(ctx context.Context) error
}
