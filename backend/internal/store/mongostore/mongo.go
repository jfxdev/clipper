// Package mongostore implements store.Store backed by MongoDB. Expiry uses
// a TTL index on the "expiresAt" field (documents that never expire simply
// omit the field, so they're skipped by the index). Burn-after-read uses a
// filtered FindOneAndDelete so the get-and-delete is atomic per document.
package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

type Config struct {
	URI        string
	Database   string
	Collection string
}

type Store struct {
	client *mongo.Client
	coll   *mongo.Collection
}

// New connects to MongoDB and ensures the TTL index exists. Index creation
// is idempotent, so it's safe to call on every startup.
func New(ctx context.Context, cfg Config) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, fmt.Errorf("mongostore: connect: %w", err)
	}
	coll := client.Database(cfg.Database).Collection(cfg.Collection)

	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "expiresAt", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	}
	if _, err := coll.Indexes().CreateOne(ctx, indexModel); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongostore: create ttl index: %w", err)
	}

	return &Store{client: client, coll: coll}, nil
}

type pasteDoc struct {
	ID                string     `bson:"_id"`
	Data              string     `bson:"data"`
	ExpiresAt         *time.Time `bson:"expiresAt,omitempty"`
	BurnAfterRead     bool       `bson:"burnAfterRead"`
	PasswordProtected bool       `bson:"passwordProtected"`
	CreatedAt         time.Time  `bson:"createdAt"`
	SizeBytes         int        `bson:"sizeBytes"`
}

func toDoc(p paste.Paste) pasteDoc {
	d := pasteDoc{
		ID:                p.ID,
		Data:              p.Data,
		BurnAfterRead:     p.BurnAfterRead,
		PasswordProtected: p.PasswordProtected,
		CreatedAt:         p.CreatedAt,
		SizeBytes:         p.SizeBytes,
	}
	if !p.ExpiresAt.IsZero() {
		t := p.ExpiresAt
		d.ExpiresAt = &t
	}
	return d
}

func (d pasteDoc) toPaste() paste.Paste {
	p := paste.Paste{
		ID:                d.ID,
		Data:              d.Data,
		BurnAfterRead:     d.BurnAfterRead,
		PasswordProtected: d.PasswordProtected,
		CreatedAt:         d.CreatedAt,
		SizeBytes:         d.SizeBytes,
	}
	if d.ExpiresAt != nil {
		p.ExpiresAt = *d.ExpiresAt
	}
	return p
}

func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	_, err := s.coll.InsertOne(ctx, toDoc(p))
	return err
}

func (s *Store) Get(ctx context.Context, id string) (paste.Paste, error) {
	var doc pasteDoc

	// Atomic burn: only matches (and deletes) a document that is both
	// present and marked burn-after-read, so concurrent readers can never
	// both succeed.
	err := s.coll.FindOneAndDelete(ctx, bson.M{"_id": id, "burnAfterRead": true}).Decode(&doc)
	switch {
	case err == nil:
		// burned successfully
	case errors.Is(err, mongo.ErrNoDocuments):
		// Either the paste doesn't exist, or it exists but isn't
		// burn-after-read — a plain read distinguishes the two without
		// deleting anything.
		err = s.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return paste.Paste{}, store.ErrNotFound
		}
		if err != nil {
			return paste.Paste{}, err
		}
	default:
		return paste.Paste{}, err
	}

	p := doc.toPaste()
	if p.IsExpired(time.Now()) {
		// Defensive re-check: the TTL index sweep runs on its own
		// schedule, so this keeps behavior identical to the other Store
		// backends regardless of when MongoDB physically removes it.
		_, _ = s.coll.DeleteOne(ctx, bson.M{"_id": id})
		return paste.Paste{}, store.ErrNotFound
	}
	return p, nil
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}
