package cli

import (
	"fmt"
	"reasonix/internal/state/sessionstore"

	"github.com/charmbracelet/x/ansi"
)

// sessionPickerLabel is the "N turns · display title" line, truncated to fit.
// Explicit session renames win, then topic titles, then the raw preview.
func sessionPickerLabel(s sessionstore.SessionInfo) string {
	preview := s.CustomTitle
	if preview == "" {
		preview = s.TopicTitle
	}
	if preview == "" {
		preview = s.Preview
	}
	if preview == "" {
		preview = "(no user message yet)"
	}
	return recoverySessionBadge(s) + fmt.Sprintf("%d turns · %s", s.Turns, ansi.Truncate(preview, 60, "…"))
}
