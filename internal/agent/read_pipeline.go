package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
)

func (a *Agent) readPipelineActive() bool {
	return a != nil && a.turn.readShadow.enabled && !a.legacyImplicitFullReads
}

func (a *Agent) legacyReadFinal(state *turnRuntime) (string, *IncompleteReadError) {
	if a.readPipelineActive() {
		return "", nil
	}
	return state.incompleteReads.blockFinal()
}

func (a *Agent) issueReadContinuation(tr readcoord.Transition, env tool.ReadResultEnvelope) {
	if a.reads.tasks == nil || env.Source.Snapshot == "" {
		return
	}
	next := 0
	if len(tr.Missing) > 0 {
		next = tr.Missing[0].Start
	} else if len(tr.Covered) > 0 {
		next = tr.Covered[len(tr.Covered)-1].End
	}
	end := 0
	if ob, ok := a.turn.readShadow.coord.Get(tr.Key); ok && len(ob.Requirement.Ranges) > 0 {
		end = ob.Requirement.Ranges[len(ob.Requirement.Ranges)-1].End
	}
	cursor := tool.ReadCursor{ReadID: tr.Key, Path: env.Source.CanonicalPath, Snapshot: env.Source.Snapshot, SessionID: a.reads.tasks.sessionID, RunGen: a.reads.tasks.generation, NextStart: next, RequestEnd: end}
	cursor.Binding = a.reads.tasks.binding
	env.NextCursor = tool.EncodeReadCursor(cursor)
	a.reads.tasks.remember(tr.Key, env)
}

func tReadObserver(a *Agent, call provider.ToolCall) (tool.ModelTextObserver, bool) {
	t, _, _ := a.svc.tools.ResolveCall(call.Name)
	o, ok := t.(tool.ModelTextObserver)
	return o, ok
}

func (a *Agent) readTimeRemaining(id string) time.Duration {
	remaining := readcoord.DefaultPolicy().MaxActiveTime
	if a.readPipelineActive() && id != "" {
		if ob, ok := a.turn.readShadow.coord.Get(id); ok {
			remaining -= ob.ActiveTime
		}
	}
	return max(time.Millisecond, remaining)
}

func (a *Agent) gateReadOperation(_ context.Context, plan *toolCallPlan) (string, bool) {
	if !a.readPipelineActive() {
		return a.turn.incompleteReads.gate(plan)
	}
	if plan.evidenceName == "session_read_strategy_receipt" {
		args, parsed := parseReadStrategyReceiptArgs(plan.evidenceArgs)
		if parsed && a.turn.readShadow.strategyPending(args.ReadID) {
			plan.incompleteReadRoot = args.ReadID
			plan.incompleteReadAction = incompleteReadActionStrategyReceipt
			return "", false
		}
	}
	if plan.evidenceName == "grep" {
		args, parsed := parseIncompleteReadGrepArgs(plan.execArgs)
		if parsed {
			for _, key := range a.turn.readShadow.strategyKeys() {
				if a.turn.readShadow.strategyPending(key) && a.strategyPathMatches(key, args.Path) {
					plan.incompleteReadRoot = key
					plan.incompleteReadAction = incompleteReadActionStrategySearch
					return "", false
				}
			}
		}
	}
	if plan.evidenceName != "read_file" {
		return "", false
	}
	path := readPathArg(plan.execArgs)
	if resolver, ok := plan.execTool.(tool.ReadPathResolver); ok {
		resolved, err := resolver.ResolveReadPath(plan.execArgs)
		if err != nil {
			return "error: " + err.Error(), true
		}
		path = resolved
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(a.writeWorkspaceRoot, path)
	}
	path = filepath.Clean(path)
	for _, ob := range a.turn.readShadow.coord.Snapshot() {
		if ob.State.Terminal() || (ob.Key != plan.readTaskID && ob.Scope.CanonicalPath != path) {
			continue
		}
		if ob.State == readcoord.StateBlocked || ob.State == readcoord.StateNeedsScope {
			args, _ := parseReadFileArgs(plan.execArgs)
			if plan.readTaskID == "" && args.LimitExplicit {
				if a.turn.readShadow.strategyPending(ob.Key) {
					plan.incompleteReadRoot = ob.Key
					plan.incompleteReadAction = incompleteReadActionStrategyRead
				}
				return "", false
			}
			return "blocked: automatic read is paused; inspect an explicit range or report the unresolved full-file requirement", true
		}
		plan.readTaskID = ob.Key
		return "", false
	}
	return "", false
}

func (a *Agent) strategyPathMatches(key, path string) bool {
	path = filepath.Clean(path)
	a.turn.incompleteReads.mu.Lock()
	defer a.turn.incompleteReads.mu.Unlock()
	e := a.turn.incompleteReads.entries[key]
	return e != nil && (filepath.Clean(e.path) == path || filepath.Clean(e.requestPath) == path)
}

// readContinuation owns the execution decision. Status rendering is only a
// projection of this state; the legacy machine is used solely in rollback mode.
func (a *Agent) readContinuation(final bool) (string, error) {
	var paused []string
	reads := a.turn.readShadow.coord.Snapshot()
	// Hard stops have priority over a continuation on another file.
	for _, ob := range reads {
		if !ob.State.Terminal() && ob.Stop != nil && !a.turn.readShadow.strategyPending(ob.Key) {
			paused = append(paused, fmt.Sprintf("%s: %s; %s", ob.Scope.CanonicalPath, ob.Stop.Detail, ob.Stop.Recovery))
		}
	}
	if final && len(paused) > 0 {
		return "", &IncompleteReadError{Reason: strings.Join(paused, "; ")}
	}
	for _, ob := range reads {
		if ob.Stop != nil && !a.turn.readShadow.strategyPending(ob.Key) {
			// A budget/policy Stop is terminal; strategy-armed reads are handled
			// below as an explicit targeted recovery path.
			continue
		}
		if ob.Stop != nil && a.turn.readShadow.strategyPending(ob.Key) {
			instruction, pause := a.strategyContinuation(final, ob.Key)
			if pause != nil {
				return "", pause
			}
			if instruction != "" {
				return instruction, nil
			}
			continue
		}
		if ob.State.Terminal() {
			continue
		}
		if ob.Stop != nil {
			continue
		}
		// A model-requested full intent is the existing explicit completeness
		// contract. Do not infer new obligations from user prose or previews.
		if final && !ob.Requirement.WholeFile {
			continue
		}
		if !final && a.turn.readShadow.hinted[ob.Key] == ob.Sequence {
			continue
		}
		if final {
			// A final answer that supplies no required content is also a stalled
			// attempt, so ignoring the instruction cannot create an infinite loop.
			tr, _ := a.turn.readShadow.coord.Observe(tool.ReadResultEnvelope{ReadID: ob.Key, Source: tool.ReadResultSource{CanonicalPath: ob.Scope.CanonicalPath, Snapshot: ob.Version}, Intent: ob.Requirement.Intent}, 0)
			a.emitReadStatus(tr, tool.ReadResultEnvelope{Intent: ob.Requirement.Intent})
			if tr.Stop != nil {
				return "", &IncompleteReadError{Reason: tr.Stop.Detail}
			}
		}
		a.reads.tasks.mu.Lock()
		task := a.reads.tasks.byID[ob.Key]
		a.reads.tasks.mu.Unlock()
		if !task.issued || task.snapshot == "" {
			tr, _ := a.turn.readShadow.coord.Narrow(ob.Key, readcoord.Block{Code: "no_cursor", Detail: "no verifiable next page is available", Recovery: "inspect a narrower range and report that the full review remains incomplete"})
			a.emitReadStatus(tr, tool.ReadResultEnvelope{Intent: ob.Requirement.Intent})
			paused = append(paused, ob.Scope.CanonicalPath+": no verifiable next page")
			continue
		}
		path := task.argumentPath
		if path == "" {
			path = task.path
		}
		args, _ := json.Marshal(map[string]any{"path": path, "cursor": tool.EncodeReadCursor(task.cursor)})
		prefix := "Continue the outstanding read with read_file "
		if _, owed := a.turn.readShadow.pivots[ob.Key]; owed {
			prefix = "The last two pages added no content. Change strategy: use the host's exact next window instead of repeating the previous page. Call read_file "
			delete(a.turn.readShadow.pivots, ob.Key)
		}
		if a.turn.readShadow.hinted == nil {
			a.turn.readShadow.hinted = map[string]uint64{}
		}
		a.turn.readShadow.hinted[ob.Key] = ob.Sequence
		suffix := ". Independent work may continue. A partial window may be sufficient for local work; do not claim a complete review until full coverage is verified."
		return fmt.Sprintf("Read coverage path=%s snapshot=%s intent=%s. %s%s%s", ob.Scope.CanonicalPath, ob.Version, ob.Requirement.Intent, prefix, string(args), suffix), nil
	}
	if final && len(paused) > 0 {
		return "", &IncompleteReadError{Reason: strings.Join(paused, "; ")}
	}
	return "", nil
}

func (a *Agent) strategyContinuation(final bool, key string) (string, *IncompleteReadError) {
	if final {
		instruction, pause := a.turn.incompleteReads.blockFinal()
		return instruction, pause
	}
	return a.turn.incompleteReads.instructionFor(key), nil
}

func (a *Agent) closeReadStatuses() {
	if !a.readPipelineActive() {
		return
	}
	for _, ob := range a.turn.readShadow.coord.Snapshot() {
		if tr, ok := a.turn.readShadow.coord.Cancel(ob.Key); ok {
			a.emitReadStatus(tr, tool.ReadResultEnvelope{Intent: ob.Requirement.Intent})
		}
	}
}

func (a *Agent) observeFailedRead(call provider.ToolCall, out toolOutcome) {
	target, name, _ := a.svc.tools.ResolveCall(call.Name)
	if name != "read_file" {
		return
	}
	path := readPathArg(json.RawMessage(call.Arguments))
	if resolver, ok := target.(tool.ReadPathResolver); ok {
		if resolved, err := resolver.ResolveReadPath(json.RawMessage(call.Arguments)); err == nil {
			path = resolved
		}
	}
	for _, ob := range a.turn.readShadow.coord.Snapshot() {
		if ob.State.Terminal() || ob.Stop != nil || (ob.Key != out.readTaskID && ob.Scope.CanonicalPath != path) {
			continue
		}
		if out.diagnostic != nil {
			tr, _ := a.turn.readShadow.coord.Fail(ob.Key, readcoord.Block{Code: out.diagnostic.Code, Detail: out.errMsg, Recovery: out.diagnostic.Recovery})
			a.emitReadStatus(tr, tool.ReadResultEnvelope{Intent: ob.Requirement.Intent})
		} else {
			// Rejected/repeated calls deliver no source lines. They still count
			// toward the same no-progress ladder instead of bypassing its bounds.
			a.observeReadShadow(tool.ReadResultEnvelope{ReadID: ob.Key, Source: tool.ReadResultSource{CanonicalPath: ob.Scope.CanonicalPath, Snapshot: ob.Version}, Intent: ob.Requirement.Intent}, out.readActiveMillis)
		}
	}
}
