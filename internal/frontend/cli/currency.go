package cli

import (
	"fmt"
	"strings"
)

func parseCLIPricingCurrency(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "AUTO":
		return "", nil
	case "CNY":
		return "CNY", nil
	case "USD":
		return "USD", nil
	default:
		return "", fmt.Errorf("pricing currency %q: must be auto|CNY|USD", value)
	}
}

func pricingCurrencyDisplay(currency string) string {
	if strings.TrimSpace(currency) == "" {
		return "auto"
	}
	return strings.ToUpper(strings.TrimSpace(currency))
}
