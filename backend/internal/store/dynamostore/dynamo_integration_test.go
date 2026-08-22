//go:build integration

package dynamostore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jfxdev/clipper/backend/internal/store/storetest"
)

func TestDynamoStore(t *testing.T) {
	endpoint := os.Getenv("DYNAMO_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8000"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s, err := New(ctx, Config{
		Table:    "clipper_test_pastes",
		Endpoint: endpoint,
		Region:   "us-east-1",
	})
	if err != nil {
		t.Skipf("dynamodb-local not reachable at %s: %v", endpoint, err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	storetest.Run(t, s)
}
