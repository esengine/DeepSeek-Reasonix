package cli

import (
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/contract/config"
)

func compactRatioPercentageRequirement() string {
	return fmt.Sprintf("percentage above %g and below %g", config.CompactRatioMin*100, config.CompactRatioMax*100)
}

func parseCLICompactRatio(value string) (float64, error) {
	percent, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	ratio := percent / 100
	if err == nil {
		err = config.ValidateCompactRatio(ratio)
	}
	if err != nil {
		return 0, fmt.Errorf("compact ratio must be a %s: %w", compactRatioPercentageRequirement(), err)
	}
	return ratio, nil
}
