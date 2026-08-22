// Package redisstore implements store.Store backed by Redis. It stores each
// paste as a hash, sets a native Redis TTL from ExpiresAt, and uses a small
// Lua script to make burn-after-read's get-and-delete atomic.
package redisstore

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

const keyPrefix = "clipper:paste:"

func key(id string) string { return keyPrefix + id }

type Config struct {
	Addr     string
	Password string
	DB       int
}

type Store struct {
	client *redis.Client
}

func New(cfg Config) *Store {
	return &Store{
		client: redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
	}
}

func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	k := key(p.ID)
	fields := map[string]any{
		"id":                p.ID,
		"data":              p.Data,
		"burnAfterRead":     boolToStr(p.BurnAfterRead),
		"passwordProtected": boolToStr(p.PasswordProtected),
		"createdAt":         p.CreatedAt.UTC().Format(time.RFC3339Nano),
		"sizeBytes":         strconv.Itoa(p.SizeBytes),
	}
	if !p.ExpiresAt.IsZero() {
		fields["expiresAt"] = p.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, k, fields)
	if !p.ExpiresAt.IsZero() {
		ttl := time.Until(p.ExpiresAt)
		if ttl <= 0 {
			ttl = time.Millisecond
		}
		pipe.PExpire(ctx, k, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// getAndMaybeBurn atomically reads the paste hash and, if it is marked
// burn-after-read, deletes it — all within a single Lua script execution so
// concurrent readers can never both see the paste.
const getAndMaybeBurnScript = `
local key = KEYS[1]
local vals = redis.call('HGETALL', key)
if #vals == 0 then
  return nil
end
local burn = redis.call('HGET', key, 'burnAfterRead')
if burn == '1' then
  redis.call('DEL', key)
end
return vals
`

func (s *Store) Get(ctx context.Context, id string) (paste.Paste, error) {
	k := key(id)
	res, err := s.client.Eval(ctx, getAndMaybeBurnScript, []string{k}).Result()
	if errors.Is(err, redis.Nil) {
		return paste.Paste{}, store.ErrNotFound
	}
	if err != nil {
		return paste.Paste{}, err
	}

	raw, ok := res.([]interface{})
	if !ok || len(raw) == 0 {
		return paste.Paste{}, store.ErrNotFound
	}

	fields := make(map[string]string, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		fk, _ := raw[i].(string)
		fv, _ := raw[i+1].(string)
		fields[fk] = fv
	}

	p, err := fromFields(fields)
	if err != nil {
		return paste.Paste{}, err
	}

	// Defensive re-check: even though Redis will also expire the key
	// natively, this keeps behavior identical across all Store backends
	// and covers any clock-skew edge case around the exact expiry moment.
	if p.IsExpired(time.Now()) {
		_ = s.client.Del(ctx, k).Err()
		return paste.Paste{}, store.ErrNotFound
	}
	return p, nil
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Close()
}

func fromFields(f map[string]string) (paste.Paste, error) {
	p := paste.Paste{
		ID:                f["id"],
		Data:              f["data"],
		BurnAfterRead:     f["burnAfterRead"] == "1",
		PasswordProtected: f["passwordProtected"] == "1",
	}
	if v := f["createdAt"]; v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return paste.Paste{}, err
		}
		p.CreatedAt = t
	}
	if v := f["expiresAt"]; v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return paste.Paste{}, err
		}
		p.ExpiresAt = t
	}
	if v := f["sizeBytes"]; v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return paste.Paste{}, err
		}
		p.SizeBytes = n
	}
	return p, nil
}

func boolToStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
