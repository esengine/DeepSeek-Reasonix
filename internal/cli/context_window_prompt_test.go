package cli

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"reasonix/internal/config"
)

// The wizard used to write 128000 unconditionally, so a 1M-window relay was
// recorded as 128K and compacted at roughly a tenth of its real capacity.
func TestAskContextWindowAcceptsTheRealWindow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      string
		want       int
		wantSource string
	}{
		{"explicit large window", "1048576\n", 1048576, config.ContextWindowSourceExplicit},
		{"surrounding spaces", "  200000  \n", 200000, config.ContextWindowSourceExplicit},
		{"empty keeps the conservative default", "\n", defaultCustomContextWindow, config.ContextWindowSourceDefault},
		{"non-numeric keeps the default", "not-a-number\n", defaultCustomContextWindow, config.ContextWindowSourceDefault},
		{"zero keeps the default", "0\n", defaultCustomContextWindow, config.ContextWindowSourceDefault},
		{"negative keeps the default", "-5\n", defaultCustomContextWindow, config.ContextWindowSourceDefault},
		{"closed input keeps the default", "", defaultCustomContextWindow, config.ContextWindowSourceDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, source := askContextWindow(bufio.NewScanner(strings.NewReader(tc.input)), io.Discard)
			if got != tc.want || source != tc.wantSource {
				t.Errorf("askContextWindow(%q) = (%d, %q), want (%d, %q)", tc.input, got, source, tc.want, tc.wantSource)
			}
		})
	}
}
