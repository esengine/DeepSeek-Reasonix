package summary

import (
	"fmt"
	"strings"
)

// Summary renders one line per distinct word with its count.
func Summary(words []string) string {
	counts := map[string]int{}
	for _, w := range words {
		counts[w]++
	}
	var out strings.Builder
	for w, n := range counts {
		fmt.Fprintf(&out, "%s=%d\n", w, n)
	}
	return out.String()
}
