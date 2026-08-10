package cli

import (
	"bufio"
	"io"
	"strconv"
	"strings"

	"reasonix/internal/i18n"
)

// defaultCustomContextWindow is the fallback for a relay whose window we cannot
// know. It is deliberately conservative: too small only wastes context, while
// too large makes the provider reject the request outright.
const defaultCustomContextWindow = 128000

// askContextWindow prompts for the model's context window. An OpenAI-compatible
// endpoint does not report it, and a value below the real window is what makes
// compaction fire long before it needs to, so the wizard asks instead of
// recording an assumption the user never sees.
func askContextWindow(in *bufio.Scanner, w io.Writer) int {
	raw := ask(in, w, i18n.M.CustomPromptWindow, strconv.Itoa(defaultCustomContextWindow))
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return defaultCustomContextWindow
	}
	return n
}
