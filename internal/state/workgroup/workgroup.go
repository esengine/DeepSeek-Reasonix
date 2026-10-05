package workgroup

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

// Call is one tool invocation as an atom. See doc.go for the three lifecycles
// a frame stream carries and which of them are calls.
type Call struct {
	ID            string
	Source        string
	Issuer        event.ToolIssuer
	ParentID      string
	Rounds        []string
	SawDispatch   bool
	SawResult     bool
	Interruptions []string
}

// TopLevel reports a call no other call spawned. A sub-agent's work belongs to
// the call that delegated it, not beside it.
func (c Call) TopLevel() bool { return c.ParentID == "" }

// Group is a run of the assistant's own top-level calls with one producer.
type Group struct {
	ID      string
	Source  string
	Members []string
	// OpenAtClose names members still in flight when the group ended. Atomizing
	// calls already stops one landing in two groups, so a misplaced boundary
	// shows up here: the group is sealed and its member's result arrives after.
	OpenAtClose []string
	ClosedBy    string
}

// Rules is the candidate partition, spelled out so one part can be switched off
// and nothing else moves. Every field is a claim a fixture has to survive.
type Rules struct {
	// BarrierClosesAfterCall: a blocking user decision does not split the call it
	// landed in; it ends the group once that call has finished.
	BarrierClosesAfterCall bool
	// BarrierSplitsInPlace is the shape shown wrong on real logs: it seals a
	// group over a call still running. Kept as the sabotage arm.
	BarrierSplitsInPlace bool
	// HostIsTransparent: the host's own bookkeeping is not a step and does not
	// cut. Off, it fragments a turn every time the host advances a list.
	HostIsTransparent bool
	// HostCounts folds the host's bookkeeping in as the assistant's work.
	HostCounts bool
	// ProviderCounts: a provider-executed call is still the assistant working.
	ProviderCounts bool
	// UserCuts: a line the user typed ends the group and nothing merges across.
	UserCuts bool
	// SourceCuts: two producers never share a group.
	SourceCuts bool
	// PromoteChildren lifts a sub-agent's calls to top-level members.
	PromoteChildren bool
}

// V1 is the candidate under assay: one producer's consecutive assistant-owned
// work across rounds. A round does not cut, a settled say does not cut, and the
// host advancing its own list is neither a step nor a boundary.
func V1() Rules {
	return Rules{
		BarrierClosesAfterCall: true,
		HostIsTransparent:      true,
		ProviderCounts:         true,
		UserCuts:               true,
		SourceCuts:             true,
	}
}

// Member reports whether a call counts as a step in a group.
func (r Rules) Member(c Call) bool {
	if !c.TopLevel() && !r.PromoteChildren {
		return false
	}
	switch c.Issuer {
	case event.IssuedByModel:
		return true
	case event.IssuedByProvider:
		return r.ProviderCounts
	case event.IssuedByHost:
		return r.HostCounts
	}
	return false
}

// Fold atomizes calls and then groups them, in one walk: a call is settled by
// its own frames wherever they land, and the grouping only reads settled facts.
func Fold(frames []eventwire.Event, r Rules) ([]Call, []Group) {
	f := &folder{rules: r, calls: map[string]*Call{}, open: map[string]bool{}}
	for _, e := range frames {
		f.step(e)
	}
	f.close("end of record")
	return f.settled(), f.groups
}

type folder struct {
	rules   Rules
	calls   map[string]*Call
	order   []string
	groups  []Group
	cur     *Group
	open    map[string]bool
	round   string
	started bool
}

func (f *folder) close(why string) {
	if f.cur == nil || len(f.cur.Members) == 0 {
		f.cur = nil
		return
	}
	f.cur.ClosedBy = why
	for _, id := range f.cur.Members {
		if f.open[id] {
			f.cur.OpenAtClose = append(f.cur.OpenAtClose, id)
		}
	}
	f.groups = append(f.groups, *f.cur)
	f.cur = nil
}

func (f *folder) step(e eventwire.Event) {
	switch e.Kind {
	case "turn_started":
		if f.started {
			f.close("next turn")
		}
		f.started = true
	case "turn_done":
		f.close("turn done")
	case "stream_attempt":
		if e.StreamAttempt != nil && e.StreamAttempt.Action == "begin" {
			f.round = e.StreamAttempt.ID
		}
	case "approval_request", "ask_request":
		for id := range f.open {
			f.calls[id].Interruptions = append(f.calls[id].Interruptions, e.Kind)
		}
		if f.rules.BarrierSplitsInPlace {
			f.close("barrier")
		}
	case "tool_dispatch", "tool_result":
		f.tool(e)
	}
}

func (f *folder) tool(e eventwire.Event) {
	t := e.Tool
	if t == nil || t.ID == "" {
		return
	}
	c, seen := f.calls[t.ID]
	if !seen {
		// A partial dispatch is not a call: its contract says a full one follows,
		// and for a tool that ends the round none ever does. Materializing a Call
		// from it leaves one open for the rest of the record.
		if t.Partial {
			return
		}
		c = &Call{ID: t.ID, Source: e.Source, Issuer: event.ToolIssuer(t.Issuer), ParentID: t.ParentID}
		f.calls[t.ID] = c
		f.order = append(f.order, t.ID)
		f.admit(*c)
	}
	if f.round != "" && (len(c.Rounds) == 0 || c.Rounds[len(c.Rounds)-1] != f.round) {
		c.Rounds = append(c.Rounds, f.round)
	}
	if e.Kind == "tool_dispatch" {
		if t.Partial {
			return
		}
		c.SawDispatch = true
		f.open[t.ID] = true
		return
	}
	c.SawResult = true
	delete(f.open, t.ID)
	if f.rules.BarrierClosesAfterCall && len(c.Interruptions) > 0 && f.rules.Member(*c) {
		f.close("barrier settled")
	}
}

// admit decides what a newly seen call does to the open group: end it, join it,
// or pass through without touching it.
func (f *folder) admit(c Call) {
	if c.Issuer == event.IssuedByUser && f.rules.UserCuts {
		f.close("user intervention")
		return
	}
	if !f.rules.Member(c) {
		if c.Issuer == event.IssuedByHost && !f.rules.HostIsTransparent {
			f.close("host bookkeeping")
		}
		return
	}
	if f.cur != nil && f.rules.SourceCuts && f.cur.Source != c.Source {
		f.close("producer change")
	}
	if f.cur == nil {
		f.cur = &Group{ID: c.ID, Source: c.Source}
	}
	f.cur.Members = append(f.cur.Members, c.ID)
}

func (f *folder) settled() []Call {
	out := make([]Call, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, *f.calls[id])
	}
	return out
}

// DuplicateMembership names calls that landed in more than one group. A
// partition is only legal where this is empty.
func DuplicateMembership(groups []Group) string {
	seen := map[string]int{}
	for _, g := range groups {
		for _, id := range g.Members {
			seen[id]++
		}
	}
	var bad []string
	for id, n := range seen {
		if n > 1 {
			bad = append(bad, fmt.Sprintf("%s in %d groups", id, n))
		}
	}
	return strings.Join(bad, "; ")
}

// SealedOverAnOpenCall names groups closed while a member was still running.
// The member's result then belongs to no group, and a reader is left with a
// finished fold over an unfinished call.
func SealedOverAnOpenCall(groups []Group) string {
	var bad []string
	for _, g := range groups {
		if len(g.OpenAtClose) > 0 {
			bad = append(bad, fmt.Sprintf("%s sealed over %s", g.ID, strings.Join(g.OpenAtClose, ",")))
		}
	}
	return strings.Join(bad, "; ")
}

// Shape renders groups for a failure message.
func Shape(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		row := g.Source + ":" + strings.Join(g.Members, ",") + "|" + g.ClosedBy
		if len(g.OpenAtClose) > 0 {
			row += "|open:" + strings.Join(g.OpenAtClose, ",")
		}
		out = append(out, row)
	}
	return out
}
