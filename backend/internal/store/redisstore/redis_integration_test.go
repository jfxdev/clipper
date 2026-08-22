//go:build integration

package redisstore

import (
	"context"
	"os"
	"testing"

	"github.com/jfxdev/clipper/backend/internal/store/storetest"
)

func TestRedisStore(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	// docker-compose.yml runs Redis with --requirepass, so the local
	// instance is not an unauthenticated open port even on a laptop.
	password := os.Getenv("REDIS_PASSWORD")
	if password == "" {
		password = "devpassword"
	}
	s := New(Config{Addr: addr, Password: password})
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	if err := s.client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}

	storetest.Run(t, s)
}
