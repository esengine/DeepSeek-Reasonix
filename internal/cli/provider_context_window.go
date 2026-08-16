package cli

import (
	"bufio"
	"io"
	"strconv"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

// defaultCustomContextWindow is the fallback for a relay whose window we cannot
// know. It is deliberately conservative: too small only wastes context, while
// too large makes the provider reject the request outright.
const defaultCustomContextWindow = 128000

// askContextWindow prompts for the model's context window and returns it
// together with its source. An OpenAI-compatible endpoint does not report it,
// and a value below the real window is what makes compaction fire long before
// it needs to, so the wizard asks instead of recording an assumption the user
// never sees. A valid answer is explicit; an empty/invalid answer keeps the
// conservative default and marks it as a default placeholder that a
// provider-learned window may later replace.
func askContextWindow(in *bufio.Scanner, w io.Writer) (int, string) {
	raw := ask(in, w, i18n.M.CustomPromptWindow, strconv.Itoa(defaultCustomContextWindow))
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return defaultCustomContextWindow, config.ContextWindowSourceDefault
	}
	return n, config.ContextWindowSourceExplicit
}
