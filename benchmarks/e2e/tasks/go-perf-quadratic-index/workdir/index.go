package index

import "strings"

// Index maps each word to the line numbers it appears on, ascending and
// without duplicates.
func Index(lines []string) map[string][]int {
	out := map[string][]int{}
	for i, line := range lines {
		for _, w := range strings.Fields(line) {
			out[w] = appendUnique(out[w], i)
		}
	}
	return out
}

func appendUnique(xs []int, v int) []int {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
