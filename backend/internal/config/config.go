// Package config loads clipper's runtime configuration from environment
// variables. It is the single contract main.go, store, and api all read from.
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port string

	// StoreBackend selects which store.Store implementation to construct:
	// "memory", "redis", "mongo", or "dynamo".
	StoreBackend string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	MongoURI        string
	MongoDatabase   string
	MongoCollection string

	DynamoTable    string
	DynamoEndpoint string // optional, for dynamodb-local
	DynamoRegion   string

	RateLimitRPS   float64
	RateLimitBurst int

	MaxPasteSizeBytes int64

	// TrustProxy controls whether the X-Forwarded-For header is used to
	// determine the client IP for rate limiting. Only enable this behind a
	// trusted reverse proxy that sets the header itself.
	TrustProxy bool
}

func Load() (Config, error) {
	cfg := Config{
		Port:              getEnv("PORT", "8080"),
		StoreBackend:      getEnv("STORE_BACKEND", "memory"),
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		MongoURI:          getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:     getEnv("MONGO_DATABASE", "clipper"),
		MongoCollection:   getEnv("MONGO_COLLECTION", "pastes"),
		DynamoTable:       getEnv("DYNAMO_TABLE", "clipper_pastes"),
		DynamoEndpoint:    getEnv("DYNAMO_ENDPOINT", ""),
		DynamoRegion:      getEnv("DYNAMO_REGION", "us-east-1"),
		MaxPasteSizeBytes: 2 * 1024 * 1024,
	}

	var err error
	if cfg.RedisDB, err = getEnvInt("REDIS_DB", 0); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitRPS, err = getEnvFloat("RATE_LIMIT_RPS", 5); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitBurst, err = getEnvInt("RATE_LIMIT_BURST", 10); err != nil {
		return Config{}, err
	}
	maxSize, err := getEnvInt64("MAX_PASTE_SIZE_BYTES", cfg.MaxPasteSizeBytes)
	if err != nil {
		return Config{}, err
	}
	cfg.MaxPasteSizeBytes = maxSize
	if cfg.TrustProxy, err = getEnvBool("TRUST_PROXY", false); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch c.StoreBackend {
	case "memory", "redis", "mongo", "dynamo":
	default:
		return fmt.Errorf("config: invalid STORE_BACKEND %q (want memory|redis|mongo|dynamo)", c.StoreBackend)
	}
	if c.MaxPasteSizeBytes <= 0 {
		return fmt.Errorf("config: MAX_PASTE_SIZE_BYTES must be positive, got %d", c.MaxPasteSizeBytes)
	}
	if c.RateLimitRPS <= 0 {
		return fmt.Errorf("config: RATE_LIMIT_RPS must be positive, got %v", c.RateLimitRPS)
	}
	if c.RateLimitBurst <= 0 {
		return fmt.Errorf("config: RATE_LIMIT_BURST must be positive, got %d", c.RateLimitBurst)
	}
	return nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func getEnvInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return n, nil
}

func getEnvInt64(key string, def int64) (int64, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return n, nil
}

func getEnvFloat(key string, def float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return n, nil
}

func getEnvBool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return b, nil
}
