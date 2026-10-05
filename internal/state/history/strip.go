package history

import (
	"reasonix/internal/state/sessionstore"
	"strings"
)

const planModeMarkerPrefix = "[Plan mode"

// stripComposePrefixes removes what the host prepended to a user turn before it
// is indexed. The tags come from the package that injects them: the copy that
// used to live here named five of the eleven, so <workspace> and the language
// blocks went into the search index and a query for a path matched every turn
// of every session.
func stripComposePrefixes(content string) string {
	trimmed := strings.TrimSpace(sessionstore.DropLeadingTransientBlocks(content))
	if strings.HasPrefix(trimmed, planModeMarkerPrefix) {
		if _, rest, ok := strings.Cut(trimmed, "]"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return trimmed
}
