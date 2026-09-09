package checkpoint

import (
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) expirePayloadLocked(c *Checkpoint) error {
	if c == nil || c.ExpiredFilePayload {
		return nil
	}
	expired := *c
	expired.Result = c.Result.Summary()
	if expired.Result != nil {
		for i := range expired.Result.Files {
			expired.Result.Files[i].Unavailable = "expired"
		}
	}
	expired.Files = append([]FileSnap(nil), c.Files...)
	expired.CoverageGaps = append([]CoverageGap(nil), c.CoverageGaps...)
	for i := range expired.Files {
		expired.Files[i].BlobRef = ""
		expired.Files[i].Content = nil
		expired.Files[i].PayloadExpired = true
	}
	expired.ExpiredFilePayload = true
	expired.Coverage = CoveragePartial
	expired.CoverageGaps = append(expired.CoverageGaps, CoverageGap{Reason: GapExpiredPayload, Detail: "file recovery payload expired"})
	if err := s.persist(&expired); err != nil {
		return err
	}
	if s.dir != "" {
		legacyVisible := filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", c.Turn))
		if err := os.Remove(legacyVisible); err != nil && !os.IsNotExist(err) {
			_ = os.Remove(s.checkpointPath(&expired))
			return err
		}
	}
	*c = expired
	return nil
}
