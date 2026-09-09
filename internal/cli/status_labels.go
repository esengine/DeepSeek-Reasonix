package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

type readStatusState struct {
	readStatusLabel string
	frames          map[string]event.ReadStatusPayload
}

func (s *readStatusState) ingest(incoming *event.ReadStatusPayload) {
	if incoming == nil || incoming.ReadID == "" {
		return
	}
	if previous, ok := s.frames[incoming.ReadID]; ok {
		if incoming.Generation < previous.Generation || (incoming.Generation == previous.Generation && incoming.Sequence <= previous.Sequence) {
			return
		}
	}
	if s.frames == nil {
		s.frames = map[string]event.ReadStatusPayload{}
	}
	s.frames[incoming.ReadID] = *incoming
	keys := make([]string, 0, len(s.frames))
	for key := range s.frames {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var labels []string
	for _, key := range keys {
		frame := s.frames[key]
		if label := readStatusLabelText(&frame); label != "" {
			labels = append(labels, label)
		}
	}
	s.readStatusLabel = strings.Join(labels, " · ")
}

// readStatusLabelText renders the host's structured read status for the live
// status line; an inactive or unnamed frame clears it.
func readStatusLabelText(rs *event.ReadStatusPayload) string {
	if rs == nil || !rs.Active || strings.TrimSpace(rs.Path) == "" {
		return ""
	}
	file := filepath.Base(rs.Path)
	covered := ""
	if len(rs.Covered) > 0 {
		parts := make([]string, 0, len(rs.Covered))
		for _, r := range rs.Covered {
			parts = append(parts, fmt.Sprintf("%d-%d", r[0]+1, r[1]))
		}
		covered = strings.Join(parts, ", ")
	}
	switch {
	case rs.State == "blocked" || rs.State == "needs_scope":
		return fmt.Sprintf(i18n.M.ReadStatusPausedFmt, file) + "; " + i18n.M.ReadStatusRecovery
	case rs.HasMore && covered != "":
		return fmt.Sprintf(i18n.M.ReadStatusCoveredFmt, file, covered)
	case rs.HasMore:
		return fmt.Sprintf(i18n.M.ReadStatusReadingFmt, file)
	case covered != "":
		return fmt.Sprintf(i18n.M.ReadStatusDoneFmt, file, covered)
	default:
		return fmt.Sprintf(i18n.M.ReadStatusReadingFmt, file)
	}
}
