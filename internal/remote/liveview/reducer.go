package liveview

import (
	"strings"
	"sync"

	"reasonix/internal/eventwire"
)

const (
	kindTurnStarted       = "turn_started"
	kindReasoning         = "reasoning"
	kindText              = "text"
	kindMessage           = "message"
	kindToolDispatch      = "tool_dispatch"
	kindToolResult        = "tool_result"
	kindUsage             = "usage"
	kindCompactionStarted = "compaction_started"
	kindCompactionDone    = "compaction_done"
	kindToolProgress      = "tool_progress"
	kindRetrying          = "retrying"
	kindApprovalRequest   = "approval_request"
	kindAskRequest        = "ask_request"
	kindTurnDone          = "turn_done"
	kindOperationDone     = "operation_done"
)

// Reducer is safe for concurrent callers. Apply calls are serialized by the
// reducer; when Apply returns, that event is reflected in every later Snapshot.
// The zero value is ready for use.
type Reducer struct {
	mu sync.Mutex

	slots            []*slot
	currentAssistant *assistantState
	toolsByID        map[string]*toolState
	tools            []*toolState
	compactions      []*compactionState
	prompt           *eventwire.Event
	retry            *eventwire.Event
	usage            usageState
	turnArgChars     int
	active           bool
}

type slotKind uint8

const (
	slotRaw slotKind = iota
	slotAssistant
	slotTool
	slotCompaction
)

type slot struct {
	kind       slotKind
	removed    bool
	raw        *eventwire.Event
	assistant  *assistantState
	tool       *toolState
	compaction *compactionState
}

type assistantState struct {
	started       *eventwire.Event
	text          strings.Builder
	reasoning     strings.Builder
	seenText      bool
	seenReasoning bool
	lastDelta     string
	message       *eventwire.Event
}

type toolState struct {
	dispatch             eventwire.Event
	effective            eventwire.Tool
	hasFullDispatch      bool
	result               *eventwire.Event
	resultBaseDispatch   *eventwire.Event
	resultBase           *eventwire.Tool
	resultBaseHasFull    bool
	progress             strings.Builder
	progressTemplate     *eventwire.Event
	postResultDispatches []eventwire.Event
	postResultProgress   strings.Builder
	postProgressTemplate *eventwire.Event
	running              bool
}

type compactionState struct {
	event   eventwire.Event
	pending bool
	slot    *slot
}

type usageContribution struct {
	completion int
	total      int
	cost       float64
}

type usageState struct {
	count         int
	completion    int
	total         int
	cost          float64
	lastGauge     *eventwire.Event
	gaugeValue    usageContribution
	finalCurrency string
}

// Apply atomically reduces event before returning. Background-only events are
// ignored exactly as they are by Desktop's applyEvent. TurnStarted opens a new
// live view and TurnDone clears it; completed content belongs to canonical
// history rather than this current-runtime projection.
func (r *Reducer) Apply(event eventwire.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch event.Kind {
	case "memory_compiler_stats", "mcp_surface_ready":
		return
	case kindRetrying:
		copy := cloneEvent(event)
		r.retry = &copy
		return
	}

	// Desktop clears its transient retry indicator on every foreground event.
	r.retry = nil

	switch event.Kind {
	case kindTurnStarted:
		r.resetLocked()
		copy := cloneEvent(event)
		assistant := &assistantState{started: &copy}
		r.currentAssistant = assistant
		r.slots = append(r.slots, &slot{kind: slotAssistant, assistant: assistant})
		r.active = true
	case kindTurnDone, kindOperationDone:
		r.resetLocked()
	case kindText, kindReasoning:
		r.applyDeltaLocked(event)
	case kindMessage:
		r.applyMessageLocked(event)
	case kindToolDispatch:
		r.applyToolDispatchLocked(event)
	case kindToolResult:
		r.applyToolResultLocked(event)
	case kindToolProgress:
		r.applyToolProgressLocked(event)
	case kindUsage:
		r.applyUsageLocked(event)
	case kindApprovalRequest, kindAskRequest:
		copy := cloneEvent(event)
		r.prompt = &copy
		r.active = true
	case kindCompactionStarted:
		r.applyCompactionStartedLocked(event)
	case kindCompactionDone:
		r.applyCompactionDoneLocked(event)
	default:
		copy := cloneEvent(event)
		r.slots = append(r.slots, &slot{kind: slotRaw, raw: &copy})
	}
}

// Snapshot returns a deep copy of a canonical event sequence. Replaying the
// sequence through Desktop's event reducer reconstructs the current unfinished
// Turn semantics without relying on a bounded raw event log.
func (r *Reducer) Snapshot() []eventwire.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]eventwire.Event, 0, len(r.slots)+8)
	for _, item := range r.slots {
		if item == nil || item.removed {
			continue
		}
		switch item.kind {
		case slotRaw:
			if item.raw != nil {
				result = append(result, cloneEvent(*item.raw))
			}
		case slotAssistant:
			result = appendAssistantEvents(result, item.assistant)
		case slotTool:
			result = appendToolEvents(result, item.tool)
		case slotCompaction:
			if item.compaction != nil {
				result = append(result, cloneEvent(item.compaction.event))
			}
		}
	}

	// Prompt is not a timeline item in Desktop. Emitting only its latest accepted
	// payload after the item stream preserves the current prompt without keeping
	// superseded deliveries alive.
	if r.prompt != nil {
		result = append(result, cloneEvent(*r.prompt))
	}
	result = append(result, r.usage.snapshotEvents()...)
	if r.turnArgChars > 0 {
		result = append(result, eventwire.Event{
			Kind: kindToolDispatch,
			Tool: &eventwire.Tool{Name: "liveview_arg_progress", Partial: true, ArgChars: r.turnArgChars},
		})
	}
	if r.retry != nil {
		result = append(result, cloneEvent(*r.retry))
	}
	return result
}

// ClearPrompt removes the currently retained Approval or Ask only when its
// opaque ID matches promptID. Prompt resolution is a Host mutation, not an
// eventwire event, so Approve/Answer/Cancel integration must call this method
// at the same serialized mutation boundary. A delayed resolution for an older
// Prompt can never clear a newer Prompt.
func (r *Reducer) ClearPrompt(promptID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if promptID == "" || r.prompt == nil || retainedPromptID(*r.prompt) != promptID {
		return false
	}
	r.prompt = nil
	return true
}

// Reset discards the current live semantic view. Host lifecycle boundaries
// such as runtime replacement and shutdown use it when no TurnDone event will
// be emitted for the retiring runtime.
func (r *Reducer) Reset() {
	r.mu.Lock()
	r.resetLocked()
	r.mu.Unlock()
}

func (r *Reducer) resetLocked() {
	r.slots = nil
	r.currentAssistant = nil
	r.toolsByID = nil
	r.tools = nil
	r.compactions = nil
	r.prompt = nil
	r.retry = nil
	r.usage = usageState{}
	r.turnArgChars = 0
	r.active = false
}

func retainedPromptID(event eventwire.Event) string {
	switch event.Kind {
	case kindApprovalRequest:
		if event.Approval != nil {
			return event.Approval.ID
		}
	case kindAskRequest:
		if event.Ask != nil {
			return event.Ask.ID
		}
	}
	return ""
}

func (r *Reducer) assistantLocked() *assistantState {
	if r.currentAssistant != nil {
		return r.currentAssistant
	}
	assistant := &assistantState{}
	r.currentAssistant = assistant
	r.slots = append(r.slots, &slot{kind: slotAssistant, assistant: assistant})
	return assistant
}

func (r *Reducer) applyDeltaLocked(event eventwire.Event) {
	assistant := r.assistantLocked()
	delta := event.Text
	if delta == "" {
		delta = event.Reasoning
	}
	if event.Kind == kindText {
		assistant.text.WriteString(delta)
		assistant.seenText = true
	} else {
		assistant.reasoning.WriteString(delta)
		assistant.seenReasoning = true
	}
	assistant.lastDelta = event.Kind
}

func (r *Reducer) applyMessageLocked(event eventwire.Event) {
	assistant := r.currentAssistant
	if assistant == nil {
		if strings.TrimSpace(event.Text) == "" && strings.TrimSpace(event.Reasoning) == "" {
			return
		}
		assistant = r.assistantLocked()
	}
	copy := cloneEvent(event)
	assistant.message = &copy
	r.currentAssistant = nil
}

func (r *Reducer) applyToolDispatchLocked(event eventwire.Event) {
	tool := event.Tool
	if tool == nil {
		return
	}
	if tool.Partial && tool.ArgChars > 0 {
		r.turnArgChars = tool.ArgChars
	}
	if tool.Partial && tool.ID == "" {
		return
	}

	var state *toolState
	if tool.ID != "" {
		r.ensureToolMapLocked()
		state = r.toolsByID[tool.ID]
	}
	if state == nil {
		copy := cloneEvent(event)
		if copy.Tool == nil {
			return
		}
		state = &toolState{
			dispatch:        copy,
			effective:       *cloneTool(copy.Tool),
			hasFullDispatch: !tool.Partial,
			running:         true,
		}
		item := &slot{kind: slotTool, tool: state}
		r.slots = append(r.slots, item)
		r.tools = append(r.tools, state)
		if tool.ID != "" {
			r.toolsByID[tool.ID] = state
		}
		return
	}

	if tool.Partial {
		if state.running && state.effective.Args == "" && tool.ArgChars > 0 {
			state.effective.ArgChars = tool.ArgChars
		}
		return
	}

	copy := cloneEvent(event)
	next := cloneTool(copy.Tool)
	if next == nil {
		return
	}
	// Match Desktop's full-dispatch upsert: empty args and a nil profile retain
	// the card's prior values, while preview fields are replaced by the refresh.
	if next.Args == "" {
		next.Args = state.effective.Args
	}
	if next.Profile == nil {
		next.Profile = cloneProfile(state.effective.Profile)
	}
	next.ID = state.effective.ID
	next.ParentID = state.effective.ParentID
	next.Partial = false
	next.ArgChars = 0
	state.dispatch = copy
	state.effective = *next
	state.hasFullDispatch = true
	if state.result != nil {
		state.postResultDispatches = append(state.postResultDispatches, cloneEvent(event))
	}
}

func (r *Reducer) applyToolResultLocked(event eventwire.Event) {
	if event.Tool == nil {
		return
	}
	state := r.findResultToolLocked(event.Tool.ID)
	if state == nil {
		return
	}
	copy := cloneEvent(event)
	if copy.Tool == nil {
		return
	}
	if state.effective.ID != "" {
		copy.Tool.ID = state.effective.ID
	}
	baseDispatch := cloneEvent(state.dispatch)
	state.resultBaseDispatch = &baseDispatch
	state.resultBase = cloneTool(&state.effective)
	state.resultBaseHasFull = state.hasFullDispatch
	state.result = &copy
	state.progress.Reset()
	state.progressTemplate = nil
	state.postResultDispatches = nil
	state.postResultProgress.Reset()
	state.postProgressTemplate = nil
	state.running = false
}

func (r *Reducer) applyToolProgressLocked(event eventwire.Event) {
	if event.Tool == nil || event.Tool.ID == "" || r.toolsByID == nil {
		return
	}
	state := r.toolsByID[event.Tool.ID]
	if state == nil {
		return
	}
	if state.result != nil && state.result.Tool != nil {
		state.postResultProgress.WriteString(event.Tool.Output)
		copy := cloneEvent(event)
		state.postProgressTemplate = &copy
		return
	}
	state.progress.WriteString(event.Tool.Output)
	copy := cloneEvent(event)
	state.progressTemplate = &copy
}

func (r *Reducer) findResultToolLocked(id string) *toolState {
	if id != "" && r.toolsByID != nil {
		if state := r.toolsByID[id]; state != nil {
			return state
		}
	}
	for index := len(r.tools) - 1; index >= 0; index-- {
		if r.tools[index].running {
			return r.tools[index]
		}
	}
	return nil
}

func (r *Reducer) ensureToolMapLocked() {
	if r.toolsByID == nil {
		r.toolsByID = make(map[string]*toolState)
	}
}

func (r *Reducer) applyUsageLocked(event eventwire.Event) {
	if !r.active {
		return
	}
	r.turnArgChars = 0
	r.usage.add(event)
}

func (r *Reducer) applyCompactionStartedLocked(event eventwire.Event) {
	copy := cloneEvent(event)
	state := &compactionState{event: copy, pending: true}
	item := &slot{kind: slotCompaction, compaction: state}
	state.slot = item
	r.compactions = append(r.compactions, state)
	r.slots = append(r.slots, item)
}

func (r *Reducer) applyCompactionDoneLocked(event eventwire.Event) {
	for index := len(r.compactions) - 1; index >= 0; index-- {
		state := r.compactions[index]
		if !state.pending {
			continue
		}
		state.pending = false
		if event.Compaction == nil || event.Compaction.Summary == "" {
			state.slot.removed = true
			return
		}
		state.event = cloneEvent(event)
		return
	}
	if event.Compaction == nil || event.Compaction.Summary == "" {
		return
	}
	copy := cloneEvent(event)
	state := &compactionState{event: copy}
	item := &slot{kind: slotCompaction, compaction: state}
	state.slot = item
	r.compactions = append(r.compactions, state)
	r.slots = append(r.slots, item)
}

func appendAssistantEvents(dst []eventwire.Event, state *assistantState) []eventwire.Event {
	if state == nil {
		return dst
	}
	if state.started != nil {
		dst = append(dst, cloneEvent(*state.started))
	}
	appendText := func() { dst = append(dst, eventwire.Event{Kind: kindText, Text: state.text.String()}) }
	appendReasoning := func() { dst = append(dst, eventwire.Event{Kind: kindReasoning, Text: state.reasoning.String()}) }
	if state.seenText && state.seenReasoning {
		if state.message == nil && state.lastDelta == kindReasoning {
			appendText()
			appendReasoning()
		} else {
			appendReasoning()
			appendText()
		}
	} else if state.seenText {
		appendText()
	} else if state.seenReasoning {
		appendReasoning()
	}
	if state.message != nil {
		dst = append(dst, cloneEvent(*state.message))
	}
	return dst
}

func appendToolEvents(dst []eventwire.Event, state *toolState) []eventwire.Event {
	if state == nil {
		return dst
	}
	dispatch := cloneEvent(state.dispatch)
	effective := cloneTool(&state.effective)
	hasFullDispatch := state.hasFullDispatch
	if state.result != nil && state.resultBaseDispatch != nil && state.resultBase != nil {
		dispatch = cloneEvent(*state.resultBaseDispatch)
		effective = cloneTool(state.resultBase)
		hasFullDispatch = state.resultBaseHasFull
	}
	dispatch.Kind = kindToolDispatch
	dispatch.Tool = effective
	if dispatch.Tool != nil {
		dispatch.Tool.Partial = !hasFullDispatch
	}
	dst = append(dst, dispatch)
	if state.result != nil {
		dst = append(dst, cloneEvent(*state.result))
		for _, event := range state.postResultDispatches {
			dst = append(dst, cloneEvent(event))
		}
		if state.postProgressTemplate != nil {
			progress := cloneEvent(*state.postProgressTemplate)
			progress.Kind = kindToolProgress
			if progress.Tool == nil {
				progress.Tool = &eventwire.Tool{}
			}
			progress.Tool.ID = state.effective.ID
			progress.Tool.Output = state.postResultProgress.String()
			dst = append(dst, progress)
		}
	} else if state.progressTemplate != nil {
		progress := cloneEvent(*state.progressTemplate)
		progress.Kind = kindToolProgress
		if progress.Tool == nil {
			progress.Tool = &eventwire.Tool{}
		}
		progress.Tool.ID = state.effective.ID
		progress.Tool.Output = state.progress.String()
		dst = append(dst, progress)
	}
	return dst
}

func (u *usageState) add(event eventwire.Event) {
	u.count++
	value := usageValue(event.Usage)
	u.completion += value.completion
	u.total += value.total
	u.cost += value.cost
	if event.Usage != nil && event.Usage.Currency != "" {
		u.finalCurrency = event.Usage.Currency
	}
	if event.Usage == nil || strings.TrimSpace(event.Usage.Source) == "" || strings.TrimSpace(event.Usage.Source) == "executor" {
		copy := cloneEvent(event)
		u.lastGauge = &copy
		u.gaugeValue = value
	}
}

func (u usageState) snapshotEvents() []eventwire.Event {
	if u.count == 0 {
		return nil
	}
	result := make([]eventwire.Event, 0, 2)
	otherCount := u.count
	otherCompletion := u.completion
	otherTotal := u.total
	otherCost := u.cost
	if u.lastGauge != nil {
		result = append(result, cloneEvent(*u.lastGauge))
		otherCount--
		otherCompletion -= u.gaugeValue.completion
		otherTotal -= u.gaugeValue.total
		otherCost -= u.gaugeValue.cost
	}
	if otherCount > 0 {
		result = append(result, eventwire.Event{Kind: kindUsage, Usage: &eventwire.Usage{
			CompletionTokens: otherCompletion,
			TotalTokens:      otherTotal,
			Source:           "subagent",
			Cost:             otherCost,
			CostUSD:          otherCost,
			Currency:         u.finalCurrency,
		}})
	}
	return result
}

func usageValue(usage *eventwire.Usage) usageContribution {
	if usage == nil {
		return usageContribution{}
	}
	total := usage.TotalTokens
	if total <= 0 {
		prompt := usage.PromptTokens
		if prompt == 0 {
			prompt = usage.CacheHitTokens + usage.CacheMissTokens
		}
		total = prompt + usage.CompletionTokens
		if total < 0 {
			total = 0
		}
	}
	cost := usage.Cost
	if cost == 0 {
		cost = usage.CostUSD
	}
	return usageContribution{completion: usage.CompletionTokens, total: total, cost: cost}
}

func cloneEvent(in eventwire.Event) eventwire.Event {
	out := in
	out.MemoryCitations = append([]eventwire.MemoryCitation(nil), in.MemoryCitations...)
	if in.MemoryCompiler != nil {
		value := *in.MemoryCompiler
		out.MemoryCompiler = &value
	}
	out.Tool = cloneTool(in.Tool)
	out.Usage = cloneUsage(in.Usage)
	out.Approval = cloneApproval(in.Approval)
	out.Ask = cloneAsk(in.Ask)
	if in.Compaction != nil {
		value := *in.Compaction
		out.Compaction = &value
	}
	if in.Guardian != nil {
		value := *in.Guardian
		value.Usage = cloneUsage(in.Guardian.Usage)
		out.Guardian = &value
	}
	if in.Readiness != nil {
		value := *in.Readiness
		value.Missing = append([]string(nil), in.Readiness.Missing...)
		out.Readiness = &value
	}
	return out
}

func cloneTool(in *eventwire.Tool) *eventwire.Tool {
	if in == nil {
		return nil
	}
	out := *in
	out.Profile = cloneProfile(in.Profile)
	return &out
}

func cloneProfile(in *eventwire.Profile) *eventwire.Profile {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneUsage(in *eventwire.Usage) *eventwire.Usage {
	if in == nil {
		return nil
	}
	out := *in
	if in.CacheDiagnostics != nil {
		diagnostics := *in.CacheDiagnostics
		diagnostics.PrefixChangeReasons = append([]string(nil), in.CacheDiagnostics.PrefixChangeReasons...)
		out.CacheDiagnostics = &diagnostics
	}
	return &out
}

func cloneApproval(in *eventwire.Approval) *eventwire.Approval {
	if in == nil {
		return nil
	}
	out := *in
	if in.MCPTrust != nil {
		trust := *in.MCPTrust
		trust.ChangedTools = append([]string(nil), in.MCPTrust.ChangedTools...)
		trust.ToolChanges = append([]eventwire.MCPToolChange(nil), in.MCPTrust.ToolChanges...)
		trust.Readers = append([]string(nil), in.MCPTrust.Readers...)
		trust.Writers = append([]string(nil), in.MCPTrust.Writers...)
		trust.Destructive = append([]string(nil), in.MCPTrust.Destructive...)
		out.MCPTrust = &trust
	}
	return &out
}

func cloneAsk(in *eventwire.Ask) *eventwire.Ask {
	if in == nil {
		return nil
	}
	out := *in
	out.Questions = make([]eventwire.AskQuestion, len(in.Questions))
	for index, question := range in.Questions {
		out.Questions[index] = question
		out.Questions[index].Options = append([]eventwire.AskOption(nil), question.Options...)
	}
	return &out
}
