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

// runResumeCommand handles "/resume": with no argument it opens the recent
// session picker; "/resume <n>" loads that
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
		m.openResumePicker()
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
	// Snapshot before moving the lease: the outgoing session must be written
	// while this process still owns it.
	_ = m.ctrl.Snapshot()
	m.followSessionLease()
	if err := m.rebindSessionLease(target.Path); err != nil {
		m.notice("resume: " + sessionLeaseHeldNotice(err))
		return
	}
	m.ctrl.Resume(loaded, target.Path)
	m.replayActiveBranch(i18n.M.ResumedTitle)
}

// resumeArgItems completes the index argument of "/resume <n>": once past the
// command word it lists recent sessions, inserting the 1-based index and
// showing timestamp + turn count + preview as the hint. Indices match
// the picker because both window through recentSessions.
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

// sessionSummary is the "N turns · display title" line shared by the /resume
// list and its argument completion. Explicit session renames win, then topic
// titles, then the raw preview so the user can identify sessions at a glance.
func sessionSummary(s agent.SessionInfo) string {
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
	return fmt.Sprintf("%d turns · %s", s.Turns, preview)
}
