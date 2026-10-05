package serve

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/eventwire"
)

// TurnTraceFacts is what a partition would be computed FROM. It holds no
// episode, no grouping and no boundary verdict — those are the thing under
// study, and a gate comparing them would only prove both readings ran the same
// algorithm. Client-side item ids and rendering are not facts about the turn
// and never enter it.
type TurnTraceFacts struct {
	AuthoredTurn  *int
	MsgIndex      *int
	Messages      []MessageFact
	Calls         []CallFact
	Interruptions []InterruptionFact
	Terminal      string
}

// MessageFact is a settled say. Presence is recorded, never the body: what is
// under test is that a cold reload knows a say happened and who wrote it.
type MessageFact struct {
	Source       string
	HasText      bool
	HasReasoning bool
}

// CallFact is one tool call's identity and provenance. Round is the model round
// it was dispatched in — recorded as an observed fact here, never used as a
// boundary.
type CallFact struct {
	ID          string
	ParentID    string
	Issuer      string
	Source      string
	Round       string
	SawDispatch bool
	SawResult   bool
}

// InterruptionFact is a barrier that landed while a call was in flight. After
// records which call had been dispatched and not yet resulted — the fact the
// partition rules were getting wrong, kept here as an observation.
type InterruptionFact struct {
	Kind     string
	ID       string
	InsideOf string
}

// traceFacts normalizes a frame stream. live says it is a connected client's,
// carrying deltas a durable log never keeps — so a say's text is known from
// them even if the settled frame stopped materializing it. A cold reload has
// only the settled frame, and that asymmetry is what makes the comparison mean
// something rather than restate itself.
func traceFacts(frames []eventwire.Event, live bool) TurnTraceFacts {
	var f TurnTraceFacts
	f.Terminal = "eof closure"
	calls := map[string]*CallFact{}
	var order []string
	open := map[string]bool{}
	round := ""
	sawText, sawReasoning := false, false
	started := false

	for _, e := range frames {
		switch e.Kind {
		case "turn_started":
			if started {
				// A turn nobody closed, ended by the next one opening. Scope, not
				// completion: the calls collected so far are still this turn's.
				f.Terminal = "next-turn closure"
				return finish(f, calls, order)
			}
			started = true
			f.AuthoredTurn, f.MsgIndex = e.AuthoredTurn, e.MsgIndex
		case "turn_done":
			f.Terminal = "explicit"
			return finish(f, calls, order)
		case "text":
			sawText = sawText || e.Text != ""
		case "reasoning":
			sawReasoning = sawReasoning || e.Reasoning != "" || e.Text != ""
		case "stream_attempt":
			if e.StreamAttempt != nil && e.StreamAttempt.Action == "begin" {
				round = e.StreamAttempt.ID
			}
		case "message":
			m := MessageFact{Source: e.Source, HasText: e.Text != "", HasReasoning: e.Reasoning != ""}
			if live {
				// What a connected client knows, which is the bar a reload has to meet.
				m.HasText = m.HasText || sawText
				m.HasReasoning = m.HasReasoning || sawReasoning
			}
			f.Messages = append(f.Messages, m)
			sawText, sawReasoning = false, false
		case "tool_dispatch", "tool_result":
			t := e.Tool
			if t == nil || t.ID == "" {
				continue
			}
			c, ok := calls[t.ID]
			if !ok {
				c = &CallFact{ID: t.ID, ParentID: t.ParentID, Issuer: t.Issuer, Source: e.Source, Round: round}
				calls[t.ID] = c
				order = append(order, t.ID)
			}
			if e.Kind == "tool_dispatch" {
				c.SawDispatch = true
				open[t.ID] = true
			} else {
				c.SawResult = true
				delete(open, t.ID)
			}
		case "approval_request", "ask_request":
			f.Interruptions = append(f.Interruptions, InterruptionFact{
				Kind: e.Kind, ID: interruptionID(e), InsideOf: oneOpenCall(open),
			})
		}
	}
	return finish(f, calls, order)
}

// finish attaches the calls in the order they were first seen. It is a function
// because a turn ends three ways and every one of them owes the same list.
func finish(f TurnTraceFacts, calls map[string]*CallFact, order []string) TurnTraceFacts {
	for _, id := range order {
		f.Calls = append(f.Calls, *calls[id])
	}
	return f
}

func interruptionID(e eventwire.Event) string {
	if e.Approval != nil {
		return e.Approval.ID
	}
	if e.Ask != nil {
		return e.Ask.ID
	}
	return ""
}

// oneOpenCall names the call an interruption landed inside, and says so only
// when exactly one is in flight: with several open, "which call" has no answer
// and inventing one would be the guess this whole line of work is removing.
func oneOpenCall(open map[string]bool) string {
	ids := make([]string, 0, len(open))
	for id := range open {
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		slices.Sort(ids)
		return fmt.Sprintf("%d open: %s", len(ids), strings.Join(ids, ","))
	}
	return ids[0]
}

// diff renders the first place two readings disagree, so a failure names the
// fact rather than dumping two structs.
func (f TurnTraceFacts) diff(other TurnTraceFacts) string {
	a, _ := json.MarshalIndent(f, "", "  ")
	b, _ := json.MarshalIndent(other, "", "  ")
	if string(a) == string(b) {
		return ""
	}
	al, bl := strings.Split(string(a), "\n"), strings.Split(string(b), "\n")
	for i := range max(len(al), len(bl)) {
		x, y := "", ""
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("first difference at line %d:\n  live   %s\n  replay %s", i+1, x, y)
		}
	}
	return "structures differ"
}
