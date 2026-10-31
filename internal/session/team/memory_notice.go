package team

import (
	"context"
	"fmt"

	"reasonix/internal/state/teamroster"
)

// MemoryReviewNote is the user-visible result of one opted-in review.
func MemoryReviewNote(r ReviewReport) string {
	return fmt.Sprintf("Memory review: %d added, %d updated, %d removed. Review the entries in Memory settings.", r.Added, r.Edited, r.Removed)
}

// RecordMemoryChange keeps shared writes visible in the group's durable log.
func (s *Service) RecordMemoryChange(ctx context.Context, agent, group, note string) error {
	if group == "" {
		return nil
	}
	_, err := s.store.AppendLine(ctx, group, teamroster.GroupLine{From: agent, Kind: teamroster.LineEvent, Event: teamroster.EventMemory, Text: note, At: s.now()})
	if err == nil {
		s.changed()
	}
	return err
}
