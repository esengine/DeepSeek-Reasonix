package cli

import (
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/i18n"
)

const resumeListCap = 10

// recentSessions returns the newest saved sessions under dir, capped so the
// 1-based indices the list shows match what /resume <n> and its completion
// resolve. A missing dir or read error yields an empty list.
func recentSessions(dir string) []agent.SessionInfo {
	if dir == "" {
		return nil
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil {
		return nil
	}
	if len(sessions) > resumeListCap {
		sessions = sessions[:resumeListCap]
	}
	return sessions
}

// runResumeCommand handles "/resume": with no argument it lists the most recent
// saved sessions (newest first, active one marked); "/resume <n>" loads that
// session into the running controller in place — keeping the current model and
// replaying the transcript into scrollback.
func (m *chatTUI) runResumeCommand(input string) {
	sessions := recentSessions(m.ctrl.SessionDir())
	if len(sessions) == 0 {
		m.notice(i18n.M.NoSessionToResume)
		return
	}

	args := tokenizeArgs(input) // args[0] == "/resume"
	if len(args) < 2 {
		m.showSessions(sessions) // write list to scrollback (above input)
		m.openResumePicker()     // open interactive picker below
		return
	}
	if m.ctrl.Running() {
		m.notice(i18n.M.ResumeBusy)
		return
	}
	idx, err := strconv.Atoi(strings.TrimSpace(args[1]))
	if err != nil || idx < 1 || idx > len(sessions) {
		m.notice(fmt.Sprintf(i18n.M.ResumeBadIndexFmt, len(sessions)))
		return
	}
	target := sessions[idx-1]
	if target.Path == m.ctrl.SessionPath() {
		m.notice(i18n.M.ResumeAlreadyActive)
		return
	}
	loaded, err := agent.LoadSession(target.Path)
	if err != nil {
		m.notice("resume: " + err.Error())
		return
	}
	// Persist the conversation we're leaving so switching back later restores it.
	_ = m.ctrl.Snapshot()
	m.ctrl.Resume(loaded, target.Path)
	m.replayActiveBranch(i18n.M.ResumedTitle)
}

// showSessions renders the recent-session list with 1-based indices, timestamp,
// turn count and preview, marking the one currently active.
func (m *chatTUI) showSessions(sessions []agent.SessionInfo) {
	active := m.ctrl.SessionPath()
	var b strings.Builder
	b.WriteString(dim("  · " + i18n.M.ResumeListHeader + "\n"))
	for i, s := range sessions {
		marker := "  "
		if s.Path == active {
			marker = accent("› ")
		}
		fmt.Fprintf(&b, "%s%d  %s  %s\n", marker, i+1,
			s.ModTime.Local().Format("01-02 15:04"), dim(sessionSummary(s)))
	}
	m.notice(strings.TrimRight(b.String(), "\n"))
}

// resumeArgItems completes the index argument of "/resume <n>": once past the
// command word it lists recent sessions, inserting the 1-based index and
// showing timestamp + turn count + preview as the hint. Indices match
// showSessions because both window through recentSessions.
func (m *chatTUI) resumeArgItems(val string) ([]compItem, int, bool) {
	cmdEnd := strings.IndexAny(val, " \t")
	if cmdEnd < 0 || val[:cmdEnd] != "/resume" {
		return nil, 0, false
	}
	from := strings.LastIndexAny(val, " \t") + 1
	if len(strings.Fields(val[:from])) != 1 || m.ctrl == nil {
		return nil, from, true
	}
	cur := val[from:]
	var out []compItem
	for i, s := range recentSessions(m.ctrl.SessionDir()) {
		idx := strconv.Itoa(i + 1)
		if cur != "" && !strings.HasPrefix(idx, cur) {
			continue
		}
		hint := fmt.Sprintf("%s · %s", s.ModTime.Local().Format("01-02 15:04"), sessionSummary(s))
		out = append(out, compItem{label: idx, insert: idx, hint: hint})
	}
	return out, from, true
}

// sessionSummary is the "N turns · project · topicTitle/first message" line
// shared by the /resume list and its argument completion.
// When a TopicTitle is set (via /rename or desktop), it is shown instead of
// the raw preview so the user can identify sessions at a glance.
func sessionSummary(s agent.SessionInfo) string {
	preview := s.Preview
	if s.TopicTitle != "" {
		preview = s.TopicTitle
	}
	if preview == "" {
		preview = "(no user message yet)"
	}
	return fmt.Sprintf("%d turns · %s", s.Turns, preview)
}
