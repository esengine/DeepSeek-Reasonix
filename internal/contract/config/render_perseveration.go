package config

import (
	"fmt"
	"strings"
)

// renderPerseverationRetries writes the perseveration_retries line when cur is
// declared and differs from def, and reports whether it wrote one.
func renderPerseverationRetries(buf *strings.Builder, cur, def *int) bool {
	if cur == nil || (def != nil && *cur == *def) {
		return false
	}
	fmt.Fprintf(buf, "perseveration_retries = %d\n", *cur)
	return true
}
