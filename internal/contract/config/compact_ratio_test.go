package config

import (
	"errors"
	"math"
	"testing"
)

func TestCompactRatioValidationHasRangeIdentity(t *testing.T) {
	for _, ratio := range []float64{CompactRatioMin, CompactRatioMax, -1, 2, math.NaN(), math.Inf(1), math.Inf(-1)} {
		cfg := Default()
		previous := cfg.Agent.CompactRatio
		if err := cfg.SetCompactRatio(ratio); !errors.Is(err, ErrCompactRatioRange) {
			t.Fatalf("SetCompactRatio(%v) = %v, want range identity", ratio, err)
		}
		if cfg.Agent.CompactRatio != previous {
			t.Fatal("rejection changed ratio")
		}
	}
	for _, ratio := range []float64{math.Nextafter(CompactRatioMin, CompactRatioMax), 0.05, 0.999, math.Nextafter(CompactRatioMax, CompactRatioMin)} {
		if err := ValidateCompactRatio(ratio); err != nil {
			t.Fatalf("ValidateCompactRatio(%v): %v", ratio, err)
		}
	}
}
