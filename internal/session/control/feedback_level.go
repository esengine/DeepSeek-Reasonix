package control

import (
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/platform/feedback"
)

const feedbackTimeLayout = "2006-01-02 15:04"

// feedbackStanding states the install's level, progress and current limits
// from the profile the service sent, and why the limits are not the level's own.
// Everything it says is a field of the profile; nothing is read from a message.
func feedbackStanding(p *feedback.Profile, offline bool) string {
	if p == nil {
		return ""
	}
	m := i18n.M
	name := feedbackLevelName(p.Level)
	line := fmt.Sprintf(m.Feedback.LevelTopFmt, p.Level, name, p.AdoptedCount)
	if p.NextLevel != nil && p.Remaining != nil {
		line = fmt.Sprintf(m.Feedback.LevelNextFmt, p.Level, name, p.AdoptedCount, *p.Remaining, feedbackLevelName(*p.NextLevel))
	}
	l := p.EffectiveLimits
	lines := []string{line, fmt.Sprintf(m.Feedback.LimitsFmt, l.ReportsPerHour, l.ReportsPerDay, l.RepliesPerHour)}
	switch {
	case p.TrustState == feedback.TrustLegacyActive && p.TrustExpiresAt != nil:
		lines = append(lines, fmt.Sprintf(m.Feedback.TrustLegacyFmt, p.TrustExpiresAt.Local().Format("2006-01-02")))
	case p.TrustState == feedback.TrustLapsed:
		lines = append(lines, m.Feedback.TrustLapsed)
	}
	if offline {
		lines[0] += " " + m.Feedback.StandingStale
	}
	return strings.Join(lines, "\n")
}

func feedbackLevelName(level int) string {
	if name, ok := i18n.M.Feedback.LevelNames[strconv.Itoa(level)]; ok {
		return name
	}
	return "L" + strconv.Itoa(level)
}

// feedbackRefusal is the sentence for a refusal that names its window, or false
// for one that does not (an older service), which keeps its plain wording.
func feedbackRefusal(err error) (string, bool) {
	l, ok := feedback.LimitOf(err)
	if !ok || l.Limit == "" {
		return "", false
	}
	m := i18n.M
	name, known := m.Feedback.LimitNames[string(l.Limit)]
	if !known {
		name = m.Feedback.LimitUnknown
	}
	switch {
	case l.Limit == feedback.LimitReplyItem:
		return fmt.Sprintf(m.Feedback.RefusedPermanentFmt, name), true
	case !l.ResetsAt.IsZero():
		return fmt.Sprintf(m.Feedback.RefusedResetsFmt, name, l.ResetsAt.Local().Format(feedbackTimeLayout)), true
	}
	return fmt.Sprintf(m.Feedback.RefusedLaterFmt, name), true
}
