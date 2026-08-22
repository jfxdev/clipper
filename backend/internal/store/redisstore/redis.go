// Package redisstore implements store.Store backed by Redis. It stores each
// paste as a hash, sets a native Redis TTL from ExpiresAt, and uses a small
// Lua script to make burn-after-read's get-and-delete atomic.
package redisstore

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
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
	// TLS enables an encrypted connection to Redis. Certificate
	// verification always stays on: a store that holds ciphertext plus
	// expiry metadata is worth protecting from an on-path attacker, and a
	// "skip verify" escape hatch would quietly undo that.
	TLS bool
}

type Store struct {
	client *redis.Client
}

func New(cfg Config) *Store {
	opts := &redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	if cfg.TLS {
		host, _, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			host = cfg.Addr
		}
		opts.TLSConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	return &Store{client: redis.NewClient(opts)}
}

func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	k := key(p.ID)
	fields := map[string]any{
		"id":                p.ID,
		"data":              p.Data,
		"readToken":         p.ReadToken,
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

// getAndMaybeBurn atomically verifies the caller's read token and, if the
// paste is marked burn-after-read, deletes it — all within a single Lua
// script execution so concurrent readers can never both see the paste and a
// wrong token can never trigger the delete.
//
// The token comparison here is Lua's ordinary string equality rather than a
// constant-time one. The token is a 256-bit hash, so a timing oracle would
// have to resolve sub-nanosecond differences across a network round trip to
// be of any use; the comparisons that happen in Go (memory store, quota
// checks) do use crypto/subtle.
const getAndMaybeBurnScript = `
local key = KEYS[1]
local token = ARGV[1]
local vals = redis.call('HGETALL', key)
if #vals == 0 then
  return nil
end
local stored = redis.call('HGET', key, 'readToken')
if stored == false then
  stored = ''
end
if stored ~= token then
  return nil
end
local burn = redis.call('HGET', key, 'burnAfterRead')
if burn == '1' then
  redis.call('DEL', key)
end
return vals
`

func (s *Store) Get(ctx context.Context, id, readToken string) (paste.Paste, error) {
	k := key(id)
	res, err := s.client.Eval(ctx, getAndMaybeBurnScript, []string{k}, readToken).Result()
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
		ReadToken:         f["readToken"],
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
