//go:build integration

package mongostore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jfxdev/clipper/backend/internal/store/storetest"
)

func TestMongoStore(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		// Credentials match docker-compose.yml, which runs MongoDB with
		// authentication enabled rather than wide open on localhost.
		uri = "mongodb://clipper:devpassword@localhost:27017/?authSource=admin"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := New(ctx, Config{URI: uri, Database: "clipper_test", Collection: "pastes"})
	if err != nil {
		t.Skipf("mongo not reachable at %s: %v", uri, err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	if err := s.client.Ping(ctx, nil); err != nil {
		t.Skipf("mongo not reachable at %s: %v", uri, err)
	}

	storetest.Run(t, s)
}
