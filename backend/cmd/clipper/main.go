// Command clipper runs the single-binary clipper server: the REST API plus
// the embedded React SPA.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jfxdev/clipper/backend/internal/api"
	"github.com/jfxdev/clipper/backend/internal/config"
	"github.com/jfxdev/clipper/backend/internal/store"
	"github.com/jfxdev/clipper/backend/internal/store/dynamostore"
	"github.com/jfxdev/clipper/backend/internal/store/memory"
	"github.com/jfxdev/clipper/backend/internal/store/mongostore"
	"github.com/jfxdev/clipper/backend/internal/store/redisstore"
	"github.com/jfxdev/clipper/backend/internal/webembed"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s, err := newStore(ctx, cfg)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer func() { _ = s.Close(context.Background()) }()

	spa, err := webembed.Handler()
	if err != nil {
		log.Fatalf("webembed: %v", err)
	}

	handlers := api.NewHandlers(s, cfg.MaxPasteSizeBytes)
	rateLimiter := api.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst, cfg.TrustProxy)
	defer rateLimiter.Close()

	mux := http.NewServeMux()
	mux.Handle("/api/", api.NewRouter(handlers, rateLimiter))
	mux.Handle("/", spa)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("clipper listening on :%s (store=%s)", cfg.Port, cfg.StoreBackend)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// newStore constructs the store.Store implementation selected by
// cfg.StoreBackend. cfg.validate (called from config.Load) already
// restricts StoreBackend to one of these four values.
func newStore(ctx context.Context, cfg config.Config) (store.Store, error) {
	switch cfg.StoreBackend {
	case "memory":
		return memory.New(), nil
	case "redis":
		return redisstore.New(redisstore.Config{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}), nil
	case "mongo":
		return mongostore.New(ctx, mongostore.Config{
			URI:        cfg.MongoURI,
			Database:   cfg.MongoDatabase,
			Collection: cfg.MongoCollection,
		})
	case "dynamo":
		return dynamostore.New(ctx, dynamostore.Config{
			Table:    cfg.DynamoTable,
			Endpoint: cfg.DynamoEndpoint,
			Region:   cfg.DynamoRegion,
		})
	default:
		return nil, fmt.Errorf("unknown STORE_BACKEND %q", cfg.StoreBackend)
	}
}
