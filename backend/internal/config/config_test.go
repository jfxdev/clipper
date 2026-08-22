package config

import (
	"math"
	"testing"
)

func TestValidateRejectsNonFiniteRateLimitRPS(t *testing.T) {
	base := Config{
		StoreBackend:      "memory",
		MaxPasteSizeBytes: 1024,
		RateLimitBurst:    10,
	}

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
	cfg := Config{
		StoreBackend:      "memory",
		MaxPasteSizeBytes: 1024,
		RateLimitBurst:    10,
		RateLimitRPS:      5,
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() = %v, want nil", err)
	}
}
