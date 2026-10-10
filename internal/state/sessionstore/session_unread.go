package sessionstore

import "time"

// Unread reports a turn that finished after the person last looked.
func (m BranchMeta) Unread() bool { return m.FinishedAt.After(m.ViewedAt) }

// RecordSessionFinished stamps the end of a turn. unread=false is a turn the
// person ended themselves: they were there, so the stamp advances ViewedAt too.
// The stamp never precedes ViewedAt, so a clock that ran backwards cannot hide it.
func RecordSessionFinished(sessionPath string, at time.Time, unread bool) error {
	if sessionPath == "" || !sessionArtifactsHaveContent(sessionPath) {
		return nil
	}
	return UpdateBranchMeta(sessionPath, false, func(m *BranchMeta) error {
		at = at.UTC()
		if !at.After(m.ViewedAt) {
			at = m.ViewedAt.Add(time.Nanosecond)
		}
		if at.After(m.FinishedAt) {
			m.FinishedAt = at
		}
		if !unread {
			m.ViewedAt = m.FinishedAt
		}
		return nil
	})
}

// MarkSessionViewed records that the person has seen the session as it stands.
// A session that is not unread, or has no sidecar, is left untouched, so opening
// one costs no write. ViewedAt only moves forward. The unlocked pre-check can
// meet a sidecar mid-replace, so it reads with the tear-tolerant retry.
func MarkSessionViewed(sessionPath string, at time.Time) error {
	if sessionPath == "" {
		return nil
	}
	if m, ok, err := loadBranchMetaRetry(sessionPath); err != nil || !ok || !m.Unread() {
		return err
	}
	return UpdateBranchMeta(sessionPath, false, func(m *BranchMeta) error {
		at = at.UTC()
		if at.Before(m.FinishedAt) {
			at = m.FinishedAt
		}
		if at.After(m.ViewedAt) {
			m.ViewedAt = at
		}
		return nil
	})
}
