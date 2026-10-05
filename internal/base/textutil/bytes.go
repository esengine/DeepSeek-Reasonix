package textutil

import "fmt"

// HumanBytes renders a size for a person or a model to read. It rounds down, so
// a number never claims more room than there is.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	if value >= 100 {
		return fmt.Sprintf("%.0f %cB", value, "KMGTP"[exp])
	}
	return fmt.Sprintf("%.1f %cB", value, "KMGTP"[exp])
}
