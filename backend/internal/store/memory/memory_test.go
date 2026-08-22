package memory

import (
	"context"
	"testing"

	"github.com/jfxdev/clipper/backend/internal/store/storetest"
)

func TestMemoryStore(t *testing.T) {
	s := New()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	storetest.Run(t, s)
}
