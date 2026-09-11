package main

import (
	"strings"

	"reasonix/internal/sessioncatalog"
)

func recoveryOnlyHasContent(sessions []sessioncatalog.SessionRecord) bool {
	for _, session := range sessions {
		if session.Turns > 0 || strings.TrimSpace(session.Preview) != "" {
			return true
		}
	}
	return false
}

func recoveryOnlyRepresentative(sessions []sessioncatalog.SessionRecord) sessioncatalog.SessionRecord {
	if len(sessions) == 0 {
		return sessioncatalog.SessionRecord{}
	}
	if canonical := sessioncatalog.CanonicalSessionPathForTopic(sessions, ""); strings.TrimSpace(canonical) != "" {
		for _, session := range sessions {
			if session.Path == canonical {
				return session
			}
		}
	}
	best := sessions[0]
	for _, candidate := range sessions[1:] {
		if candidate.Turns != best.Turns {
			if candidate.Turns > best.Turns {
				best = candidate
			}
			continue
		}
		if candidate.LastActivityAt != best.LastActivityAt {
			if candidate.LastActivityAt > best.LastActivityAt {
				best = candidate
			}
			continue
		}
		if candidate.Path < best.Path {
			best = candidate
		}
	}
	return best
}
