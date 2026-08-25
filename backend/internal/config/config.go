// Package config loads clipper's runtime configuration from environment
// variables. It is the single contract main.go, store, and api all read from.
package config

import (
	"fmt"
	"math"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port string

	// StoreBackend selects which store.Store implementation to construct:
	// "memory", "redis", "mongo", or "dynamo".
	StoreBackend string

	// Mode restricts which API operations this instance serves: "read"
	// exposes only paste retrieval, "write" only paste creation, and the
	// empty default serves both. It lets a deployment split ingest and serve
	// nodes over the same store. The gate lives in the router; disabled
	// operations answer 403 with a generic body that never names the mode.
	Mode string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisTLS      bool

	MongoURI        string
	MongoDatabase   string
	MongoCollection string

	DynamoTable    string
	DynamoEndpoint string // optional, for dynamodb-local
	DynamoRegion   string

	// RateLimitRPS/Burst is the per-client token bucket. GlobalRateLimit*
	// is a second bucket shared by every client, so a distributed flood
	// from many source addresses still hits a ceiling.
	RateLimitRPS         float64
	RateLimitBurst       int
	GlobalRateLimitRPS   float64
	GlobalRateLimitBurst int

	// RateLimitMaxClients caps how many per-client buckets are held at
	// once. Without it, an attacker walking an IPv6 prefix can allocate
	// buckets faster than the idle sweep reclaims them.
	RateLimitMaxClients int

	// QuotaPastesPerDay/QuotaBytesPerDay bound how much storage a single
	// client can consume in a rolling 24h window, which the request-rate
	// limiter alone does not: 5 rps sustained for a day is a lot of 2MB
	// pastes.
	QuotaPastesPerDay int
	QuotaBytesPerDay  int64

	MaxPasteSizeBytes int64

	// MaxExpireSeconds caps requested lifetimes. Pastes must expire: a
	// secret with no expiry is a liability that outlives whatever made it
	// worth sharing.
	MaxExpireSeconds int64

	// TrustProxy trusts the X-Forwarded-For header from any direct peer.
	// TrustedProxies is the safer form: X-Forwarded-For is only consulted
	// when the direct peer's address falls inside one of these prefixes.
	TrustProxy     bool
	TrustedProxies []netip.Prefix

	// HSTSMaxAgeSeconds emits Strict-Transport-Security when > 0. Left off
	// by default because the binary itself speaks plain HTTP; turn it on
	// when a TLS terminator sits in front.
	HSTSMaxAgeSeconds int
}

func Load() (Config, error) {
	cfg := Config{
		Port:              getEnv("PORT", "8080"),
		StoreBackend:      getEnv("STORE_BACKEND", "memory"),
		Mode:              getEnv("MODE", ""),
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
	if cfg.RedisTLS, err = getEnvBool("REDIS_TLS", false); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitRPS, err = getEnvFloat("RATE_LIMIT_RPS", 5); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitBurst, err = getEnvInt("RATE_LIMIT_BURST", 10); err != nil {
		return Config{}, err
	}
	if cfg.GlobalRateLimitRPS, err = getEnvFloat("GLOBAL_RATE_LIMIT_RPS", 200); err != nil {
		return Config{}, err
	}
	if cfg.GlobalRateLimitBurst, err = getEnvInt("GLOBAL_RATE_LIMIT_BURST", 400); err != nil {
		return Config{}, err
	}
	if cfg.RateLimitMaxClients, err = getEnvInt("RATE_LIMIT_MAX_CLIENTS", 100_000); err != nil {
		return Config{}, err
	}
	if cfg.QuotaPastesPerDay, err = getEnvInt("QUOTA_PASTES_PER_DAY", 200); err != nil {
		return Config{}, err
	}
	if cfg.QuotaBytesPerDay, err = getEnvInt64("QUOTA_BYTES_PER_DAY", 64*1024*1024); err != nil {
		return Config{}, err
	}
	if cfg.MaxPasteSizeBytes, err = getEnvInt64("MAX_PASTE_SIZE_BYTES", cfg.MaxPasteSizeBytes); err != nil {
		return Config{}, err
	}
	if cfg.MaxExpireSeconds, err = getEnvInt64("MAX_EXPIRE_SECONDS", 30*24*60*60); err != nil {
		return Config{}, err
	}
	if cfg.HSTSMaxAgeSeconds, err = getEnvInt("HSTS_MAX_AGE_SECONDS", 0); err != nil {
		return Config{}, err
	}
	if cfg.TrustProxy, err = getEnvBool("TRUST_PROXY", false); err != nil {
		return Config{}, err
	}
	if cfg.TrustedProxies, err = getEnvPrefixes("TRUSTED_PROXIES"); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// maxExpireCeiling is the highest MAX_EXPIRE_SECONDS accepted: one year,
// comfortably under the ~9.223e9-second threshold at which
// time.Duration(seconds) * time.Second would overflow int64 nanoseconds.
const maxExpireCeiling int64 = 365 * 24 * 60 * 60

func (c Config) validate() error {
	switch c.StoreBackend {
	case "memory", "redis", "mongo", "dynamo":
	default:
		return fmt.Errorf("config: invalid STORE_BACKEND %q (want memory|redis|mongo|dynamo)", c.StoreBackend)
	}
	switch c.Mode {
	case "", "read", "write":
	default:
		return fmt.Errorf("config: invalid MODE %q (want read|write or empty)", c.Mode)
	}
	if c.MaxPasteSizeBytes <= 0 {
		return fmt.Errorf("config: MAX_PASTE_SIZE_BYTES must be positive, got %d", c.MaxPasteSizeBytes)
	}
	if c.MaxExpireSeconds <= 0 || c.MaxExpireSeconds > maxExpireCeiling {
		return fmt.Errorf("config: MAX_EXPIRE_SECONDS must be in (0, %d], got %d", maxExpireCeiling, c.MaxExpireSeconds)
	}
	// strconv.ParseFloat accepts "NaN"/"Inf"/"+Inf"/"-Inf" without error, and
	// NaN compares false to everything (including <= 0), so both must be
	// checked explicitly in addition to the sign/zero check.
	if err := validateRate("RATE_LIMIT_RPS", c.RateLimitRPS); err != nil {
		return err
	}
	if err := validateRate("GLOBAL_RATE_LIMIT_RPS", c.GlobalRateLimitRPS); err != nil {
		return err
	}
	if c.RateLimitBurst <= 0 {
		return fmt.Errorf("config: RATE_LIMIT_BURST must be positive, got %d", c.RateLimitBurst)
	}
	if c.GlobalRateLimitBurst <= 0 {
		return fmt.Errorf("config: GLOBAL_RATE_LIMIT_BURST must be positive, got %d", c.GlobalRateLimitBurst)
	}
	if c.RateLimitMaxClients <= 0 {
		return fmt.Errorf("config: RATE_LIMIT_MAX_CLIENTS must be positive, got %d", c.RateLimitMaxClients)
	}
	if c.QuotaPastesPerDay <= 0 {
		return fmt.Errorf("config: QUOTA_PASTES_PER_DAY must be positive, got %d", c.QuotaPastesPerDay)
	}
	if c.QuotaBytesPerDay <= 0 {
		return fmt.Errorf("config: QUOTA_BYTES_PER_DAY must be positive, got %d", c.QuotaBytesPerDay)
	}
	if c.HSTSMaxAgeSeconds < 0 {
		return fmt.Errorf("config: HSTS_MAX_AGE_SECONDS must not be negative, got %d", c.HSTSMaxAgeSeconds)
	}
	return nil
}

func validateRate(name string, rps float64) error {
	if math.IsNaN(rps) || math.IsInf(rps, 0) || rps <= 0 {
		return fmt.Errorf("config: %s must be a finite positive number, got %v", name, rps)
	}
	return nil
}

// Warnings lists configuration that is valid but risky to run in
// production, so main can log it loudly at startup instead of letting it
// pass silently.
func (c Config) Warnings() []string {
	var w []string
	if c.StoreBackend == "memory" {
		w = append(w, "STORE_BACKEND=memory keeps every paste in process memory: data is lost on restart and not shared between replicas. Use redis, mongo, or dynamo in production.")
		if c.Mode == "read" || c.Mode == "write" {
			w = append(w, "MODE splits reads and writes across instances, but STORE_BACKEND=memory is per-process: a write-only node's pastes are invisible to a read-only node. Use a shared backend (redis, mongo, or dynamo) with MODE.")
		}
	}
	if c.TrustProxy && len(c.TrustedProxies) == 0 {
		w = append(w, "TRUST_PROXY=true accepts X-Forwarded-For from any direct peer. If the process is reachable without going through your reverse proxy, clients can spoof their address and bypass rate limiting. Prefer TRUSTED_PROXIES with your proxy's CIDR.")
	}
	if !c.TrustProxy && len(c.TrustedProxies) == 0 {
		w = append(w, "X-Forwarded-For is ignored (no TRUSTED_PROXIES set). Behind a reverse proxy this collapses every client into one rate-limit bucket; set TRUSTED_PROXIES to your proxy's CIDR.")
	}
	if c.RedisTLS || c.StoreBackend != "redis" {
		return w
	}
	if !strings.HasPrefix(c.RedisAddr, "localhost:") && !strings.HasPrefix(c.RedisAddr, "127.0.0.1:") {
		w = append(w, "REDIS_TLS is off for a non-local REDIS_ADDR: paste ciphertext and metadata cross the network in plaintext.")
	}
	return w
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

// getEnvPrefixes parses a comma-separated list of CIDR prefixes. A bare
// address is accepted and treated as a single-host prefix, since that is
// how people usually write "my one proxy".
func getEnvPrefixes(key string) ([]netip.Prefix, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(part); err == nil {
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("config: invalid %s entry %q: not a CIDR prefix or IP address", key, part)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}
