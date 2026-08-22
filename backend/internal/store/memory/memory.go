// Package memory provides an in-memory store.Store implementation. It is
// intended for tests only — it has no persistence and is not safe to run in
// production.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

// janitorInterval controls how often expired entries are swept from the
// map. Get also defensively checks expiry on every call, so this interval
// only affects how quickly memory is reclaimed, not correctness.
const janitorInterval = 30 * time.Second

type Store struct {
	mu     sync.Mutex
	pastes map[string]paste.Paste

	stop chan struct{}
	once sync.Once
}

func New() *Store {
	s := &Store{
		pastes: make(map[string]paste.Paste),
		stop:   make(chan struct{}),
	}
	go s.janitor()
	return s
}

func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pastes[p.ID] = p
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (paste.Paste, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.pastes[id]
	if !ok {
		return paste.Paste{}, store.ErrNotFound
	}
	if p.IsExpired(time.Now()) {
		delete(s.pastes, id)
		return paste.Paste{}, store.ErrNotFound
	}
	if p.BurnAfterRead {
		delete(s.pastes, id)
	}
	return p, nil
}

func (s *Store) Close(ctx context.Context) error {
	s.once.Do(func() { close(s.stop) })
	return nil
}

func (s *Store) janitor() {
	ticker := time.NewTicker(janitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.sweep()
		}
	}
}

func (s *Store) sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.pastes {
		if p.IsExpired(time.Now()) {
			delete(s.pastes, id)
		}
	}
}
