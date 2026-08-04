package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/i18n"
)

const resumeListCap = 10

// recentSessions returns the newest saved sessions under dir. It keeps recovery
// groups intact at the display cap (a single group may make the result slightly
// larger) so the 1-based indices match /resume <n> and its completion without
// orphaning a conflict copy from its parent. A read error yields an empty list.
func recentSessions(dir string) []agent.SessionInfo {
	if dir == "" {
		return nil
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil {
		return nil
	}
	sessions = orderResumeSessions(sessions)
	return capResumeSessionGroups(sessions, resumeListCap)
}

// mostRecentSession returns the chronologically newest saved session for
// --continue. Interactive resume surfaces deliberately group recovery families
// and prefer visible leaves, but --continue promises the most recent session and
// must not let that presentation ordering select an older recovery copy.
func mostRecentSession(dir string) (agent.SessionInfo, bool) {
	if dir == "" {
		return agent.SessionInfo{}, false
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil || len(sessions) == 0 {
		return agent.SessionInfo{}, false
	}
	return sessions[0], true
}

func capResumeSessionGroups(sessions []agent.SessionInfo, limit int) []agent.SessionInfo {
	if limit <= 0 || len(sessions) <= limit {
		return sessions
	}
	byID := make(map[string]agent.SessionInfo, len(sessions))
	for _, session := range sessions {
		byID[agent.BranchID(session.Path)] = session
	}
	out := make([]agent.SessionInfo, 0, limit)
	for start := 0; start < len(sessions); {
		key := recoveryResumeGroupKey(sessions[start], byID)
		end := start + 1
		for end < len(sessions) && recoveryResumeGroupKey(sessions[end], byID) == key {
			end++
		}
		if len(out) > 0 && len(out)+(end-start) > limit {
			break
		}
		out = append(out, sessions[start:end]...)
		start = end
		if len(out) >= limit {
			break
		}
	}
	return out
}

// orderResumeSessions keeps conflict-recovery copies next to the session they
// came from. Groups remain newest-first, while the newest visible leaf is first
// within each group so interactive picker and numbered resume surfaces present
// the most likely writable continuation before its ancestors.
func orderResumeSessions(sessions []agent.SessionInfo) []agent.SessionInfo {
	if len(sessions) < 2 {
		return sessions
	}
	byID := make(map[string]agent.SessionInfo, len(sessions))
	for _, session := range sessions {
		byID[agent.BranchID(session.Path)] = session
	}
	type resumeGroup struct {
		items    []agent.SessionInfo
		newest   int
		activity int64
	}
	groups := make(map[string]*resumeGroup, len(sessions))
	order := make([]*resumeGroup, 0, len(sessions))
	for i, session := range sessions {
		key := recoveryResumeGroupKey(session, byID)
		group := groups[key]
		if group == nil {
			group = &resumeGroup{newest: i}
			groups[key] = group
			order = append(order, group)
		}
		group.items = append(group.items, session)
		if stamp := session.ModTime.UnixNano(); stamp > group.activity {
			group.activity = stamp
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].activity == order[j].activity {
			return order[i].newest < order[j].newest
		}
		return order[i].activity > order[j].activity
	})

	out := make([]agent.SessionInfo, 0, len(sessions))
	for _, group := range order {
		children := make(map[string]bool, len(group.items))
		members := make(map[string]bool, len(group.items))
		for _, session := range group.items {
			members[agent.BranchID(session.Path)] = true
		}
		for _, session := range group.items {
			parentID := strings.TrimSpace(session.ParentID)
			if members[parentID] {
				children[parentID] = true
			}
		}
		sort.SliceStable(group.items, func(i, j int) bool {
			iLeaf := !children[agent.BranchID(group.items[i].Path)]
			jLeaf := !children[agent.BranchID(group.items[j].Path)]
			if iLeaf != jLeaf {
				return iLeaf
			}
			return group.items[i].ModTime.After(group.items[j].ModTime)
		})
		out = append(out, group.items...)
	}
	return out
}

func recoveryResumeGroupKey(session agent.SessionInfo, byID map[string]agent.SessionInfo) string {
	id := agent.BranchID(session.Path)
	if !session.Recovered {
		return id
	}
	seen := map[string]bool{id: true}
	current := session
	for {
		parentID := strings.TrimSpace(current.ParentID)
		if parentID == "" {
			return agent.BranchID(current.Path)
		}
		if seen[parentID] {
			return "recovery-cycle:" + parentID
		}
		seen[parentID] = true
		parent, ok := byID[parentID]
		if !ok {
			return "recovery-parent:" + parentID
		}
		if !parent.Recovered {
			return parentID
		}
		current = parent
	}
}

// runResumeCommand handles "/resume": with no argument it opens the recent
// session picker; "/resume <n>" loads that
// session into the running controller in place — keeping the current model and
// replaying the transcript into scrollback.
func (m *chatTUI) runResumeCommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/resume"
	if len(args) < 2 {
		m.openResumePicker()
		return
	}
	// Do not run recovery GC between displaying/completing a numeric index and
	// resolving it here. Removing an earlier row would silently retarget the
	// user's already-selected number. Bare /resume performs cleanup before it
	// builds the picker, and startup performs the ordinary background sweep.
	sessions := recentSessions(m.ctrl.SessionDir())
	if len(sessions) == 0 {
		m.notice(i18n.M.NoSessionToResume)
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
	return recoverySessionBadge(s) + fmt.Sprintf("%d turns · %s", s.Turns, preview)
}

func recoverySessionBadge(s agent.SessionInfo) string {
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
