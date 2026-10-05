package report

import (
	"sort"
	"strconv"
	"strings"
)

// Build renders one line per row, prefixed by its rank.
func Build(rows []string) string {
	ranked := rank(rows)
	out := ""
	for i, r := range ranked {
		out += pad(i+1) + " " + r + "\n"
	}
	return out
}

// rank sorts a copy of rows. Every comparison lowercases both sides, so it
// allocates twice per comparison — the most expensive-looking code here.
func rank(rows []string) []string {
	cp := append([]string(nil), rows...)
	sort.SliceStable(cp, func(a, b int) bool {
		return strings.ToLower(cp[a]) < strings.ToLower(cp[b])
	})
	return cp
}

// pad left-pads n to four columns.
func pad(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 4 {
		s = " " + s
	}
	return s
}
