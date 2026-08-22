// Command clipper runs the single-binary clipper server: the REST API plus
// the embedded React SPA.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

// maxHeaderBytes is well under net/http's 1MB default. Nothing this service
// accepts needs large headers, and a smaller ceiling means less memory a
// client can pin per connection before being told no.
const maxHeaderBytes = 16 * 1024

func main() {
	// The production image is FROM scratch: no shell, no curl, nothing to
	// run a health probe with. The binary probes itself instead, which
	// keeps the image free of a package manager and everything that comes
	// with one.
	healthcheck := flag.Bool("healthcheck", false, "probe the local server's health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(runHealthcheck())
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	api.SetErrorLogger(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", slog.Any("error", err))
		os.Exit(1)
	}
	for _, warning := range cfg.Warnings() {
		logger.Warn("insecure configuration", slog.String("detail", warning))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s, err := newStore(ctx, cfg)
	if err != nil {
		logger.Error("store", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() { _ = s.Close(context.Background()) }()

	spa, err := webembed.Handler()
	if err != nil {
		logger.Error("webembed", slog.Any("error", err))
		os.Exit(1)
	}

	quota := api.NewQuota(api.QuotaConfig{
		MaxPastes:      cfg.QuotaPastesPerDay,
		MaxBytes:       cfg.QuotaBytesPerDay,
		MaxClients:     cfg.RateLimitMaxClients,
		TrustProxy:     cfg.TrustProxy,
		TrustedProxies: cfg.TrustedProxies,
	})
	defer quota.Close()

	handlers := api.NewHandlers(s, api.HandlersConfig{
		MaxPasteSizeBytes: cfg.MaxPasteSizeBytes,
		MaxExpireSeconds:  cfg.MaxExpireSeconds,
		Quota:             quota,
	})

	rateLimiter := api.NewRateLimiter(api.RateLimiterConfig{
		RPS:            cfg.RateLimitRPS,
		Burst:          cfg.RateLimitBurst,
		GlobalRPS:      cfg.GlobalRateLimitRPS,
		GlobalBurst:    cfg.GlobalRateLimitBurst,
		MaxClients:     cfg.RateLimitMaxClients,
		TrustProxy:     cfg.TrustProxy,
		TrustedProxies: cfg.TrustedProxies,
	})
	defer rateLimiter.Close()

	mux := http.NewServeMux()
	mux.Handle("/api/", api.NewRouter(handlers, rateLimiter))
	mux.Handle("GET /.well-known/security.txt", webembed.SecurityTxtHandler())
	mux.Handle("/", spa)

	// Outermost first: every response — including panics, 404s and static
	// assets — must carry the security headers, and every request must be
	// traceable to a log line.
	handler := api.RequestID(
		api.AccessLog(logger, cfg.TrustProxy, cfg.TrustedProxies)(
			api.Recover(logger)(
				api.SecurityHeaders(api.SecurityHeadersConfig{
					HSTSMaxAgeSeconds: cfg.HSTSMaxAgeSeconds,
				})(mux),
			),
		),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("clipper listening",
		slog.String("addr", srv.Addr),
		slog.String("store", cfg.StoreBackend),
	)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server", slog.Any("error", err))
		os.Exit(1)
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
			TLS:      cfg.RedisTLS,
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

// isNumericPort reports whether s is a decimal port number in range.
func isNumericPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > 0 && n <= 65535
}

// runHealthcheck performs the container health probe and returns the
// process exit code.
func runHealthcheck() int {
	// PORT is validated rather than interpolated as-is: it is the one piece
	// of this URL that comes from the environment, and a probe that can be
	// pointed at an arbitrary host by setting an env var is a request
	// forgery primitive, however unlikely the path.
	port := os.Getenv("PORT")
	if !isNumericPort(port) {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	url := "http://" + net.JoinHostPort("127.0.0.1", port) + "/api/health"
	// #nosec G704 -- the host is the literal loopback address and the only
	// variable part, the port, is validated numeric above; there is no
	// input that can redirect this probe at another host.
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
