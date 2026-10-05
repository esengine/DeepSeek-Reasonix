package update

import (
	"strconv"
	"strings"
)

// TotalFromContentRange parses the total size out of "bytes 200-999/1000",
// returning 0 when it is absent or "*" (unknown). It drives download progress.
func TotalFromContentRange(v string) int64 {
	i := strings.LastIndex(v, "/")
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v[i+1:]), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// RangeStartFromContentRange parses the first byte offset out of
// "bytes 200-999/1000". Anything unparseable answers false, which callers must
// treat as "cannot prove this continues my download".
func RangeStartFromContentRange(v string) (int64, bool) {
	unit, spec, ok := strings.Cut(strings.TrimSpace(v), " ")
	// The unit must be bytes: this offset is about to authorize appending to a
	// partial file, so anything else is "cannot prove", not "close enough".
	if !ok || !strings.EqualFold(unit, "bytes") {
		return 0, false
	}
	start, _, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(start), 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
