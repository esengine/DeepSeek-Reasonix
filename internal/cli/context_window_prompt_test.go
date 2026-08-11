package cli

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

// The wizard used to write 128000 unconditionally, so a 1M-window relay was
// recorded as 128K and compacted at roughly a tenth of its real capacity.
func TestAskContextWindowAcceptsTheRealWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  int
	}{
		{"explicit large window", "1048576\n", 1048576},
		{"surrounding spaces", "  200000  \n", 200000},
		{"empty keeps the conservative default", "\n", defaultCustomContextWindow},
		{"non-numeric keeps the default", "not-a-number\n", defaultCustomContextWindow},
		{"zero keeps the default", "0\n", defaultCustomContextWindow},
		{"negative keeps the default", "-5\n", defaultCustomContextWindow},
		{"closed input keeps the default", "", defaultCustomContextWindow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := askContextWindow(bufio.NewScanner(strings.NewReader(tc.input)), io.Discard)
			if got != tc.want {
				t.Errorf("askContextWindow(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}
