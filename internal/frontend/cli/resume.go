package cli

import (
	"fmt"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/base/i18n"
)

// mostRecentSession returns the chronologically newest saved session for
// --continue. Interactive resume surfaces deliberately group recovery families
// and prefer visible leaves, but --continue promises the most recent session and
// must not let that presentation ordering select an older recovery copy.
func mostRecentSession(dir string) (sessionstore.SessionInfo, bool) {
	if dir == "" {
		return sessionstore.SessionInfo{}, false
	}
	sessions, err := sessionstore.ListSessions(dir)
	if err != nil || len(sessions) == 0 {
		return sessionstore.SessionInfo{}, false
	}
	return sessions[0], true
}

func recoverySessionBadge(s sessionstore.SessionInfo) string {
	if !s.Recovered {
		return ""
	}
	parent := strings.TrimSpace(s.ParentID)
	if len(parent) > 8 {
		parent = parent[:8]
	}
	if parent == "" {
		parent = "?"
	}
	return fmt.Sprintf(i18n.M.ResumeRecoveryBadgeFmt, parent) + " "
}
