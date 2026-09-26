package tui

import (
	"slices"
	"strings"

	"reasonix/internal/contract/eventwire"
)

// ItemKind is what a transcript row is.
type ItemKind int

const (
	ItemUser ItemKind = iota
	ItemSay
	ItemTool
	ItemApproval
	ItemAsk
	ItemNotice
	ItemCompaction
	ItemReceipt
	// ItemUsage is what one model request cost.
	ItemUsage
)

// Item is one row of the transcript. Which fields are set follows Kind.
type Item struct {
	ID   int
	Kind ItemKind

	// ItemUser. Pending is input the kernel has not taken into a turn yet;
	// Steer is input a running turn read at a tool boundary. MsgIndex is the
	// kernel's name for the message once a turn has started on it.
	Text     string
	Pending  bool
	Steer    bool
	QueueID  string
	MsgIndex int

	// ItemSay.
	Reasoning string
	Done      bool
	ThoughtMs int64
	// Framed is set once the kernel's message frame has settled the answer; a
	// dispatch closes one before its frame arrives.
	Framed bool

	// ItemTool. Children are a sub-agent's calls, folded under its task.
	Tool     *eventwire.Tool
	Children []eventwire.Tool
	Running  bool
	Fold     outputFold // an ItemSay's thinking uses it too

	// ItemApproval / ItemAsk. Verdict is how this screen settled it; empty
	// while it is still open.
	Approval *eventwire.Approval
	Ask      *eventwire.Ask
	Verdict  string

	// ItemNotice. Count folds a repeated notice into the row already there.
	Level string
	Code  string
	Count int

	Compaction *eventwire.Compaction
	Receipt    *eventwire.CompletionReceipt
	Usage      *eventwire.Usage
}

// outputFold is how much of a finished shell call's output, or of an answer's
// thinking, its row shows: fixed where the rows cannot be redrawn, else it opens.
type outputFold int8

const (
	foldFixed outputFold = iota
	foldShut
	foldOpen
)

// Terminal is how the last turn ended.
type Terminal int

const (
	TurnOpen Terminal = iota
	TurnCompleted
	TurnCancelled
	TurnFailed
	TurnIncomplete
)

// Transcript folds the event stream the way Studio's session reducer does, so
// a conversation reads the same in both. Nothing on the wire echoes what the
// user typed: the client adds its own row (AddUser).
type Transcript struct {
	Items    []Item
	Running  bool
	Terminal Terminal
	// EndReason is the kernel's account of a turn that did not complete.
	EndReason string
	Usage     *eventwire.Usage
	// Phase is the kernel's name for what the running turn is doing, and
	// TurnOut the output tokens its requests have billed so far.
	Phase   string
	TurnOut int
	// TodosMoved says the kernel's task list changed and should be re-read.
	TodosMoved bool
	// QueueMoved says the durable input queue changed and should be re-read.
	QueueMoved bool
	// awaiting are rows this screen sent that no turn has started on yet, in
	// the order sent. Steered input is not among them: a steer event takes it.
	awaiting []int
	next     int
}

func (t *Transcript) id() int {
	t.next++
	return t.next
}

// AddUser records a message this screen sent to start a turn.
func (t *Transcript) AddUser(text string) int {
	id := t.id()
	t.Items = append(t.Items, Item{ID: id, Kind: ItemUser, Text: text})
	t.awaiting = append(t.awaiting, id)
	return id
}

// AddQueued records input handed to a running turn. It stays pending until the
// kernel takes it: a steer event for guidance, the start of its own turn for a
// follow-up.
func (t *Transcript) AddQueued(text string, steer bool) int {
	id := t.id()
	t.Items = append(t.Items, Item{ID: id, Kind: ItemUser, Text: text, Pending: true})
	if !steer {
		t.awaiting = append(t.awaiting, id)
	}
	return id
}

// AddNotice records something this screen has to say, such as a request the
// kernel refused.
func (t *Transcript) AddNotice(level, text string) {
	t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemNotice, Level: level, Text: text})
}

// SetQueueID names a pending row by the id the kernel queued it under, which
// is what the steer event that consumes it will carry.
func (t *Transcript) SetQueueID(row int, queueID string) {
	for i := range t.Items {
		if t.Items[i].ID == row {
			t.Items[i].QueueID = queueID
			return
		}
	}
}

// Drop removes a row the kernel never took: it is not part of what happened.
func (t *Transcript) Drop(row int) {
	t.Items = slices.DeleteFunc(t.Items, func(it Item) bool { return it.ID == row })
	t.awaiting = slices.DeleteFunc(t.awaiting, func(id int) bool { return id == row })
}

// Decide seals an approval or ask this screen answered.
func (t *Transcript) Decide(id int, verdict string) {
	for i := range t.Items {
		if t.Items[i].ID == id {
			t.Items[i].Verdict = verdict
			return
		}
	}
}

// OpenPrompt is the approval or ask still waiting on an answer, if any.
func (t *Transcript) OpenPrompt() *Item {
	for i, it := range slices.Backward(t.Items) {
		if (it.Kind == ItemApproval || it.Kind == ItemAsk) && it.Verdict == "" {
			return &t.Items[i]
		}
	}
	return nil
}

// Apply folds one frame.
func (t *Transcript) Apply(ev eventwire.Event) {
	// A decision receipt is state synchronisation, not conversation: the card it
	// names settles, and the audit wording is not repeated as a row.
	if ev.DecisionReceipt != nil {
		t.sealByReceipt(ev.DecisionReceipt)
	}
	if ev.Kind == "notice" && ev.Code == "decision_receipt" {
		return
	}
	switch ev.Kind {
	case "turn_started":
		t.Running, t.Terminal, t.EndReason = true, TurnOpen, ""
		t.Phase, t.TurnOut = "", 0
		t.nameTurnStart(ev)
	case "reasoning":
		t.appendSay(ev.Text, true)
	case "text":
		t.appendSay(ev.Text, false)
	case "message":
		t.foldMessage(ev)
	case "tool_dispatch", "tool_progress":
		if ev.Tool != nil {
			t.foldTool(*ev.Tool, true)
		}
	case "tool_result":
		if ev.Tool != nil {
			t.foldTool(*ev.Tool, false)
		}
	case "approval_request":
		if ev.Approval != nil {
			t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemApproval, Approval: ev.Approval})
		}
	case "ask_request":
		if ev.Ask != nil {
			t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemAsk, Ask: ev.Ask})
		}
	case "compaction_started":
		t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemCompaction, Compaction: ev.Compaction})
	case "compaction_done":
		for i, it := range slices.Backward(t.Items) {
			if it.Kind == ItemCompaction {
				t.Items[i].Compaction, t.Items[i].Done = ev.Compaction, true
				break
			}
		}
	case "steer":
		t.foldSteer(ev)
	case "todo_progress":
		t.TodosMoved = true
	case "inbox_changed":
		t.QueueMoved = true
	case "usage":
		t.Usage = ev.Usage
		t.foldUsage(ev.Usage)
	case "turn_phase":
		t.Phase = ev.Phase
	case "notice":
		t.foldNotice(ev)
	case "turn_done":
		t.Running = false
		t.Terminal, t.EndReason = terminalOf(ev)
		t.sealSays()
		t.sealTools(ev.Err != "")
		if ev.Receipt != nil && ev.Receipt.SaysSomething {
			t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemReceipt, Receipt: ev.Receipt})
		}
	}
}

func terminalOf(ev eventwire.Event) (Terminal, string) {
	switch {
	case ev.Cancelled:
		return TurnCancelled, ev.Err
	case ev.Err != "":
		return TurnFailed, ev.Err
	case ev.Outcome != "":
		return TurnIncomplete, ev.Outcome
	}
	return TurnCompleted, ""
}

func (t *Transcript) openSay() int {
	if n := len(t.Items); n > 0 && t.Items[n-1].Kind == ItemSay && !t.Items[n-1].Done {
		return n - 1
	}
	return -1
}

func (t *Transcript) appendSay(text string, reasoning bool) {
	at := t.openSay()
	if at < 0 {
		t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemSay})
		at = len(t.Items) - 1
	}
	if reasoning {
		t.Items[at].Reasoning += text
	} else {
		t.Items[at].Text += text
	}
}

// foldMessage settles this turn's latest unframed answer, open or closed by
// the call after it, with the frame's text: the record wins over the deltas,
// so a dropped chunk is repaired. With no such answer (a rebuild, or deltas
// that never came) the frame is the answer.
func (t *Transcript) foldMessage(ev eventwire.Event) {
	at := -1
	for i, it := range slices.Backward(t.Items) {
		if it.Kind == ItemUser && !it.Pending {
			break
		}
		if it.Kind == ItemSay && !it.Framed {
			at = i
			break
		}
	}
	if at < 0 {
		if ev.Text == "" && ev.Reasoning == "" {
			return
		}
		t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemSay, Text: ev.Text, Reasoning: ev.Reasoning, Done: true, Framed: true, ThoughtMs: ev.ThoughtMs})
		return
	}
	it := &t.Items[at]
	it.Done, it.Framed = true, true
	if ev.Text != "" {
		it.Text = ev.Text
	}
	if ev.Reasoning != "" {
		it.Reasoning = ev.Reasoning
	}
	if ev.ThoughtMs > 0 {
		it.ThoughtMs = ev.ThoughtMs
	}
}

// mergeTool lays a later frame of the same call over the earlier one. The wire
// omits empty fields, so a result does not erase the arguments its dispatch
// carried, and partial is set only by the frame that says so.
func mergeTool(prev, next eventwire.Tool) eventwire.Tool {
	out := prev
	if next.Name != "" {
		out.Name = next.Name
	}
	if next.Args != "" {
		out.Args = next.Args
	}
	if next.Output != "" {
		out.Output = next.Output
	}
	if next.Err != "" {
		out.Err = next.Err
	}
	if next.RefusalCode != "" {
		out.RefusalCode = next.RefusalCode
	}
	if next.Diff != "" {
		out.Diff, out.Added, out.Removed = next.Diff, next.Added, next.Removed
	}
	if next.DurationMs > 0 {
		out.DurationMs = next.DurationMs
	}
	if next.Execution != nil {
		out.Execution = next.Execution
	}
	if next.Bound != nil {
		out.Bound = next.Bound
	}
	out.Partial = next.Partial
	return out
}

func (t *Transcript) foldTool(tool eventwire.Tool, running bool) {
	if tool.ParentID != "" {
		for i := range t.Items {
			it := &t.Items[i]
			if it.Kind != ItemTool || it.Tool.ID != tool.ParentID {
				continue
			}
			for k := range it.Children {
				if it.Children[k].ID == tool.ID {
					it.Children[k] = mergeTool(it.Children[k], tool)
					return
				}
			}
			it.Children = append(it.Children, tool)
			return
		}
	}
	if tool.ID != "" {
		for i := range t.Items {
			if it := &t.Items[i]; it.Kind == ItemTool && it.Tool.ID == tool.ID {
				merged := mergeTool(*it.Tool, tool)
				it.Tool, it.Running = &merged, running
				return
			}
		}
	}
	// A dispatch closes the answer before it: the model moved on to a call.
	if at := t.openSay(); at >= 0 {
		t.Items[at].Done = true
	}
	t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemTool, Tool: &tool, Running: running})
}

// nameTurnStart seats the message a turn began on. The oldest row this screen
// sent and no turn has taken is the one: a follow-up that waited in the queue
// moves to where its turn began. With none waiting, another client started
// the turn, and the kernel's text is the only account of what was said.
func (t *Transcript) nameTurnStart(ev eventwire.Event) {
	if ev.AuthoredTurn == nil || ev.MsgIndex == nil {
		return
	}
	for len(t.awaiting) > 0 {
		id := t.awaiting[0]
		t.awaiting = t.awaiting[1:]
		at := slices.IndexFunc(t.Items, func(it Item) bool { return it.ID == id })
		if at < 0 {
			continue
		}
		row := t.Items[at]
		row.MsgIndex = *ev.MsgIndex
		if !row.Pending {
			t.Items[at] = row
			return
		}
		row.Pending = false
		t.Items = append(append(t.Items[:at:at], t.Items[at+1:]...), row)
		return
	}
	text := strings.TrimSpace(ev.Text)
	if text == "" || slices.ContainsFunc(t.Items, func(it Item) bool { return it.Kind == ItemUser && it.MsgIndex == *ev.MsgIndex }) {
		return
	}
	t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemUser, Text: text, MsgIndex: *ev.MsgIndex})
}

// foldSteer moves pending input to where the turn read it: the work that ran
// while it waited was not done about it.
func (t *Transcript) foldSteer(ev eventwire.Event) {
	at := -1
	for i, it := range t.Items {
		if it.Kind != ItemUser || !it.Pending {
			continue
		}
		if (ev.ItemID != "" && it.QueueID == ev.ItemID) || (ev.ItemID == "" && it.Text == ev.Text) {
			at = i
			break
		}
	}
	if at < 0 {
		if strings.TrimSpace(ev.Text) != "" && !ev.HostAuthored {
			t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemUser, Text: ev.Text, Steer: true})
		}
		return
	}
	row := t.Items[at]
	row.Pending, row.Steer = false, true
	t.Items = append(append(t.Items[:at:at], t.Items[at+1:]...), row)
}

// foldNotice keeps notices about the machine out of the conversation unless
// they need attention now, and folds a repeat into the row it repeats.
func (t *Transcript) foldNotice(ev eventwire.Event) {
	level := ev.Level
	if level == "" {
		level = "info"
	}
	if ev.Audience == "operator" && level == "info" {
		return
	}
	if n := len(t.Items); n > 0 {
		last := &t.Items[n-1]
		same := last.Kind == ItemNotice && last.Level == level &&
			((last.Code != "" && last.Code == ev.Code) || (last.Code == "" && ev.Code == "" && last.Text == ev.Text))
		if same {
			last.Count = max(last.Count, 1) + 1
			return
		}
	}
	t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemNotice, Level: level, Code: ev.Code, Text: ev.Text})
}

func (t *Transcript) sealSays() {
	for i := range t.Items {
		if t.Items[i].Kind == ItemSay {
			t.Items[i].Done = true
		}
	}
}

// sealTools stops every call still drawn as running: a turn that ended owns no
// call in flight, and a failed turn's open call did not finish.
func (t *Transcript) sealTools(failed bool) {
	for i := range t.Items {
		it := &t.Items[i]
		if it.Kind == ItemTool && it.Running {
			it.Running = false
			if failed && it.Tool.Err == "" && it.Tool.Output == "" {
				it.Tool.Err = "interrupted"
			}
		}
	}
}

// sealByReceipt settles a prompt answered somewhere else: the kernel's receipt
// names it, and this screen must stop offering an answer to it.
func (t *Transcript) sealByReceipt(r *eventwire.DecisionReceipt) {
	for i := range t.Items {
		it := &t.Items[i]
		if it.Verdict != "" {
			continue
		}
		if (it.Kind == ItemApproval && it.Approval.ID == r.ID) || (it.Kind == ItemAsk && it.Ask.ID == r.ID) {
			it.Verdict = "elsewhere"
		}
	}
}

// foldUsage records a request's usage. A later frame of the same attempt
// restates it rather than billing it again.
func (t *Transcript) foldUsage(u *eventwire.Usage) {
	if u == nil || u.TotalTokens == 0 {
		return
	}
	if u.AttemptID != "" {
		for i, it := range slices.Backward(t.Items) {
			if it.Kind != ItemUsage {
				continue
			}
			if it.Usage.AttemptID == u.AttemptID {
				t.TurnOut += u.CompletionTokens - it.Usage.CompletionTokens
				t.Items[i].Usage = u
				return
			}
			break
		}
	}
	t.TurnOut += u.CompletionTokens
	at := len(t.Items)
	for at > 0 && isCallRow(t.Items[at-1].Kind) {
		at--
	}
	t.Items = slices.Insert(t.Items, at, Item{ID: t.id(), Kind: ItemUsage, Usage: u})
}

// isCallRow is a row a request's calls produce. The request's usage goes
// above them, under the answer that asked for them, where the request ended.
func isCallRow(k ItemKind) bool {
	return k == ItemTool || k == ItemApproval || k == ItemAsk
}
