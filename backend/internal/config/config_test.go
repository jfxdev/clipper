package config

import (
	"math"
	"strings"
	"testing"
)

func TestValidateRejectsNonFiniteRateLimitRPS(t *testing.T) {
	base := validConfig()

	cases := map[string]float64{
		"NaN":      math.NaN(),
		"+Inf":     math.Inf(1),
		"-Inf":     math.Inf(-1),
		"zero":     0,
		"negative": -5,
	}
	for name, rps := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.RateLimitRPS = rps
			if err := cfg.validate(); err == nil {
				t.Fatalf("validate() with RateLimitRPS=%v: want error, got nil", rps)
			}
		})
	}
}

func TestValidateAcceptsFinitePositiveRateLimitRPS(t *testing.T) {
	cfg := validConfig()
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() = %v, want nil", err)
	}
}

func TestValidateMode(t *testing.T) {
	base := validConfig()
	for _, mode := range []string{"", "read", "write"} {
		cfg := base
		cfg.Mode = mode
		if err := cfg.validate(); err != nil {
			t.Fatalf("validate() with Mode=%q: want nil, got %v", mode, err)
		}
	}
	for _, mode := range []string{"readwrite", "READ", "rw", "delete"} {
		cfg := base
		cfg.Mode = mode
		if err := cfg.validate(); err == nil {
			t.Fatalf("validate() with Mode=%q: want error, got nil", mode)
		}
	}
}

// validConfig is the smallest configuration that passes validate(), so each
// test can vary exactly the field it is about.
func validConfig() Config {
	return Config{
		StoreBackend:         "memory",
		MaxPasteSizeBytes:    1024,
		MaxExpireSeconds:     30 * 24 * 60 * 60,
		RateLimitRPS:         5,
		RateLimitBurst:       10,
		GlobalRateLimitRPS:   100,
		GlobalRateLimitBurst: 100,
		RateLimitMaxClients:  10,
		QuotaPastesPerDay:    10,
		QuotaBytesPerDay:     1024,
	}
}

func TestValidateRejectsUnboundedRetention(t *testing.T) {
	base := validConfig()
	for name, seconds := range map[string]int64{
		"zero":          0,
		"negative":      -1,
		"above ceiling": maxExpireCeiling + 1,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.MaxExpireSeconds = seconds
			if err := cfg.validate(); err == nil {
				t.Fatalf("validate() with MaxExpireSeconds=%d: want error, got nil", seconds)
			}
		})
	}

	cfg := base
	cfg.MaxExpireSeconds = 30 * 24 * 60 * 60
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() = %v, want nil", err)
	}
}

// TestWarningsFlagRiskyDefaults: these configurations are valid but should
// never pass unnoticed in production.
func TestWarningsFlagRiskyDefaults(t *testing.T) {
	cfg := Config{StoreBackend: "memory"}
	if len(cfg.Warnings()) == 0 {
		t.Error("memory store produced no warning")
	}

	blunt := Config{StoreBackend: "redis", RedisAddr: "localhost:6379", TrustProxy: true}
	var sawTrustProxy bool
	for _, w := range blunt.Warnings() {
		if strings.Contains(w, "TRUST_PROXY=true") {
			sawTrustProxy = true
		}
	}
	if !sawTrustProxy {
		t.Errorf("TRUST_PROXY=true with no CIDR list produced no warning: %v", blunt.Warnings())
	}

	plaintextRedis := Config{StoreBackend: "redis", RedisAddr: "redis.internal:6379", TrustedProxies: nil}
	var sawTLS bool
	for _, w := range plaintextRedis.Warnings() {
		if strings.Contains(w, "REDIS_TLS") {
			sawTLS = true
		}
	}
	if !sawTLS {
		t.Errorf("plaintext remote redis produced no warning: %v", plaintextRedis.Warnings())
	}
}
