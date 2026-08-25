package config

import (
	"os"
	"testing"
)

// clipperEnvKeys is every environment variable Load reads. clearEnv unsets
// them (restoring afterwards) so a test can assert Load's defaults without
// the ambient shell or CI leaking values in.
var clipperEnvKeys = []string{
	"PORT", "STORE_BACKEND", "MODE",
	"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "REDIS_TLS",
	"MONGO_URI", "MONGO_DATABASE", "MONGO_COLLECTION",
	"DYNAMO_TABLE", "DYNAMO_ENDPOINT", "DYNAMO_REGION",
	"RATE_LIMIT_RPS", "RATE_LIMIT_BURST",
	"GLOBAL_RATE_LIMIT_RPS", "GLOBAL_RATE_LIMIT_BURST",
	"RATE_LIMIT_MAX_CLIENTS", "QUOTA_PASTES_PER_DAY", "QUOTA_BYTES_PER_DAY",
	"MAX_PASTE_SIZE_BYTES", "MAX_EXPIRE_SECONDS", "HSTS_MAX_AGE_SECONDS",
	"TRUST_PROXY", "TRUSTED_PROXIES",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range clipperEnvKeys {
		if orig, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { _ = os.Setenv(k, orig) })
			_ = os.Unsetenv(k)
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.StoreBackend != "memory" {
		t.Errorf("StoreBackend = %q, want memory", cfg.StoreBackend)
	}
	if cfg.Mode != "" {
		t.Errorf("Mode = %q, want empty", cfg.Mode)
	}
	if cfg.RateLimitRPS != 5 || cfg.RateLimitBurst != 10 {
		t.Errorf("rate limit defaults = %v/%d, want 5/10", cfg.RateLimitRPS, cfg.RateLimitBurst)
	}
	if cfg.MaxExpireSeconds != 30*24*60*60 {
		t.Errorf("MaxExpireSeconds = %d, want 30d", cfg.MaxExpireSeconds)
	}
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("STORE_BACKEND", "redis")
	t.Setenv("MODE", "read")
	t.Setenv("REDIS_DB", "3")
	t.Setenv("REDIS_TLS", "true")
	t.Setenv("RATE_LIMIT_RPS", "12.5")
	t.Setenv("RATE_LIMIT_BURST", "20")
	t.Setenv("QUOTA_BYTES_PER_DAY", "1048576")
	t.Setenv("HSTS_MAX_AGE_SECONDS", "63072000")
	t.Setenv("TRUST_PROXY", "true")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if cfg.Port != "9090" || cfg.StoreBackend != "redis" || cfg.Mode != "read" {
		t.Errorf("basic fields = %q/%q/%q", cfg.Port, cfg.StoreBackend, cfg.Mode)
	}
	if cfg.RedisDB != 3 || !cfg.RedisTLS {
		t.Errorf("redis fields = %d/%v", cfg.RedisDB, cfg.RedisTLS)
	}
	if cfg.RateLimitRPS != 12.5 || cfg.RateLimitBurst != 20 {
		t.Errorf("rate limit = %v/%d", cfg.RateLimitRPS, cfg.RateLimitBurst)
	}
	if cfg.QuotaBytesPerDay != 1048576 || cfg.HSTSMaxAgeSeconds != 63072000 {
		t.Errorf("quota/hsts = %d/%d", cfg.QuotaBytesPerDay, cfg.HSTSMaxAgeSeconds)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("TrustedProxies = %v, want 2 entries", cfg.TrustedProxies)
	}
	// A bare IP is normalized to a single-host prefix.
	if got := cfg.TrustedProxies[1].String(); got != "192.168.1.1/32" {
		t.Errorf("bare IP prefix = %q, want 192.168.1.1/32", got)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]struct{ key, val string }{
		"bad int":     {"REDIS_DB", "notanumber"},
		"bad int64":   {"MAX_PASTE_SIZE_BYTES", "huge"},
		"bad float":   {"RATE_LIMIT_RPS", "fast"},
		"bad bool":    {"REDIS_TLS", "maybe"},
		"bad mode":    {"MODE", "readwrite"},
		"bad backend": {"STORE_BACKEND", "sqlite"},
		"bad prefix":  {"TRUSTED_PROXIES", "not-a-cidr"},
		"nonfinite":   {"RATE_LIMIT_RPS", "Inf"},
		"unbounded":   {"MAX_EXPIRE_SECONDS", "999999999999"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() with %s=%q: want error, got nil", tc.key, tc.val)
			}
		})
	}
}
