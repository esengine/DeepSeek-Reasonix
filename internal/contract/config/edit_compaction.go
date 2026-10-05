// edit_compaction.go — the two bounds that decide when a session folds. They
// are set independently and only the lower one fires, so they are edited in one
// place rather than beside unrelated settings.
package config

import (
	"errors"
	"fmt"
)

const (
	CompactRatioMin = 0.0
	CompactRatioMax = 1.0
)

var ErrCompactRatioRange = errors.New("compact ratio outside the open window fraction range")

func ValidateCompactRatio(ratio float64) error {
	if !(ratio > CompactRatioMin && ratio < CompactRatioMax) {
		return fmt.Errorf("%w: %v must be above %g and below %g", ErrCompactRatioRange, ratio, CompactRatioMin, CompactRatioMax)
	}
	return nil
}

// SetCompactRatio updates the sole automatic compaction threshold. Presets are
// 0.70 / 0.80 / 0.85; any fraction of the window is allowed.
func (c *Config) SetCompactRatio(ratio float64) error {
	if err := ValidateCompactRatio(ratio); err != nil {
		return err
	}
	c.Agent.CompactRatio = ratio
	return nil
}

// SetContextSoftLimitTokens updates the economic maintenance boundary: the
// visible input size a fold happens at whatever the declared window allows.
// Zero uses the model-capacity policy. Positive values add an earlier fixed
// boundary; negative values remain accepted for compatibility and also mean
// capacity-based maintenance.
func (c *Config) SetContextSoftLimitTokens(tokens int) error {
	c.Agent.ContextSoftLimitTokens = tokens
	return nil
}
