package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/fileops"
	"reasonix/internal/jobs"
	"reasonix/internal/mcpinteraction"
	"reasonix/internal/memory"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// executeOne runs a single tool call. It is pure with respect to the event sink
// — the caller emits ToolDispatch/ToolResult — so it is safe to invoke fromparallel goroutines. Stages:
// parse → policy → prepare → finish.
func (a *Agent) executeOne(ctx context.Context, turn *turnRuntime, call provider.ToolCall) (out toolOutcome) {
	defer func() { out.runState = outcomeRunState(out) }()
	ctx = fileops.WithStore(withTurnState(a.withAgentContext(ctx), turn), a.fileObservations)
	plan := &toolCallPlan{call: call}
	defer func() {
		out.evidenceSource = cloneEvidenceTarget(plan.expectedWriteSource)
		out.readTaskID = plan.readTaskID
		out.readEnvelope = plan.readEnvelope
		out.readActiveMillis = plan.readActiveMillis
		if plan.mutationObserved && !plan.mutationAfterDone {
			a.observeAfterMutation(plan)
		}
		if plan.releaseMutationWrite != nil {
			plan.releaseMutationWrite()
		}
		if plan.releaseParentWrite != nil {
			plan.releaseParentWrite()
		}
		if plan.releaseLease != nil {
			plan.releaseLease()
		}
		if plan.resolvedMeta == nil {
			return
		}
		out.readTaskID = plan.readTaskID
		out.resolved = true
		out.resolvedName = plan.resolvedMeta.TargetName
		out.capabilityID = plan.resolvedMeta.CapabilityID
		out.resolvedReadOnly = plan.resolvedMeta.ReadOnly
	}()
	defer finalizeWorkspaceMutationOutcome(&out, plan)

	if blocked, early := a.parseToolCall(ctx, turn, plan); early {
		return blocked
	}
	if blocked, early := a.resolveToolPolicy(ctx, turn, plan); early {
		return blocked
	}
	if blocked, early := a.prepareToolExecution(ctx, plan); early {
		return blocked
	}
	if blocked, early := a.checkToolRecoveryStart(ctx, plan); early {
		return blocked
	}
	return a.finishToolExecution(ctx, plan)
}

// resolveToolPolicy applies Plan mode, proxy resolution, delivery gates, Auto
// Guard, and permission checks. Permission must complete before any write lease.
func (a *Agent) resolveToolPolicy(ctx context.Context, turn *turnRuntime, plan *toolCallPlan) (toolOutcome, bool) {
	if blocked, early := a.applyPlanModeAndProxy(ctx, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyResolvedTargetGates(plan); early {
		return blocked, true
	}
	// Resolve and validate before invoking the extension sidecar. A replacement
	// is resolved and validated again before permission, hooks, leases, process
	// startup, or tools/call.
	originalName, originalArgs := plan.call.Name, plan.call.Arguments
	if blocked, early := a.interceptToolBefore(ctx, plan); early {
		return blocked, true
	}
	if plan.call.Name != originalName || plan.call.Arguments != originalArgs {
		replacement := plan.call
		*plan = toolCallPlan{call: replacement}
		if blocked, early := a.parseToolCall(ctx, turn, plan); early {
			return blocked, true
		}
		if blocked, early := a.applyPlanModeAndProxy(ctx, plan); early {
			return blocked, true
		}
		if blocked, early := a.applyResolvedTargetGates(plan); early {
			return blocked, true
		}
	}
	if blocked, early := a.commitResolvedSkip(plan); early {
		return blocked, true
	}
	if blocked, early := a.applyContextualToolGate(ctx, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyExecutionPreflight(turn, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyRecoveryAndPermission(ctx, plan); early {
		return blocked, true
	}
	return toolOutcome{}, false
}

func (a *Agent) applyContextualToolGate(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if plan == nil || plan.tool == nil {
		return toolOutcome{}, false
	}
	if outcome, blocked := contextualToolGateOutcome(ctx, plan.tool, plan.canonicalName); blocked {
		return outcome, true
	}
	if plan.execTool != nil {
		if outcome, blocked := contextualToolGateOutcome(ctx, plan.execTool, plan.permName); blocked {
			return outcome, true
		}
	}
	return toolOutcome{}, false
}

func contextualToolGateOutcome(ctx context.Context, target tool.Tool, name string) (toolOutcome, bool) {
	contextual, ok := target.(tool.ContextualTool)
	if !ok || contextual.ProviderVisible(ctx) {
		return toolOutcome{}, false
	}
	msg := fmt.Sprintf("blocked: tool %q is unavailable in the current workflow context", name)
	switch name {
	case "get_goal", "create_goal", "update_goal":
		msg = "goal tools require the current top-level host-attested goal context — no goal state was changed"
	case "bash_output", "wait", "kill_shell":
		msg = "background jobs are not available in this context"
	}
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg)}, true
}

// applyPlanModeAndProxy handles initial Plan mode, proxy resolution / skip path,
// resolved-target Plan re-check, and MCP Plan availability.
func (a *Agent) applyPlanModeAndProxy(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	t := plan.tool
	call := plan.call
	if a.planMode.Load() {
		// Translate the tool's optional plan-mode self-report into the policy'stri-state.
		// Mirrorsthet.(tool.Previewer) assertion precedent below.
		safety := planmode.PlanSafetyUnknown
		if c, ok := t.(tool.PlanModeClassifier); ok {
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			} else {
				safety = planmode.PlanSafetyUnsafe
			}
		}
		if decision := a.planModeDecision(plan.canonicalName, t.ReadOnly(), safety, json.RawMessage(call.Arguments)); decision.Blocked {
			return toolOutcome{
				output:  decision.Message,
				blocked: true,
				errMsg:  "blocked: tool is unavailable during planning",
			}, true
		}
	}
	// Resolve proxy tools (use_capability) to the real MCP target beforepermission, hooks, and evidence.
	// Provider transcript keeps call.Name.
	if resolver, ok := t.(tool.CallResolver); ok {
		rc, rerr := resolver.ResolveCall(ctx, json.RawMessage(call.Arguments))
		if rerr != nil {
			return a.proxyResolutionError(plan, rerr), true
		}
		plan.resolved = rc
		plan.resolvedMeta = &plan.resolved
		if rc.TargetName != "" {
			plan.permName = rc.TargetName
			plan.evidenceName = rc.TargetName
		}
		// An unavailable resolution has no concrete target contract. Keep the
		// original proxy arguments so host validation checks use_capability itself
		// and the deterministic disabled/unregistered reason remains intact.
		if len(rc.Args) > 0 && !rc.Unavailable {
			plan.permArgs = rc.Args
			plan.evidenceArgs = rc.Args
			plan.execArgs = rc.Args
		}
		if rc.Target != nil {
			plan.execTool = rc.Target
		}
		if outcome, blocked := contextualToolGateOutcome(ctx, plan.execTool, plan.permName); blocked {
			return outcome, true
		}
		plan.readOnly = rc.ReadOnly
		plan.classifyEffects()
		if outcome, blocked := a.readOnlyExecutionBlock(t, &rc); blocked {
			return blockedShellOutcome(outcome, plan), true
		}
	} else if outcome, blocked := a.readOnlyExecutionBlock(t, nil); blocked {
		return blockedShellOutcome(outcome, plan), true
	}

	// A proxy resolution can point at atargetwithanexplicitplanning-phaseopt-outeventhoughtheproxyitselfhasnone.
	// Re-check the resolved targetbefore its ordinary permission and sandbox path.
	if plan.resolved.TargetName != "" && a.planMode.Load() {
		safety := planmode.PlanSafetyUnknown
		if c, ok := plan.execTool.(tool.PlanModeClassifier); ok {
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			} else {
				safety = planmode.PlanSafetyUnsafe
			}
		}
		if decision := a.planModeDecision(plan.permName, plan.resolved.ReadOnly, safety, plan.permArgs); decision.Blocked {
			return toolOutcome{
				output:  decision.Message,
				blocked: true,
				errMsg:  "blocked: tool is unavailable during planning",
			}, true
		}
	}
	plannerTrustedMCP := a.plannerMCPExecution && isMCPExecutionTarget(plan.execTool, plan.permName) && mcpServerAuthorized(plan.execTool) && !mcpDestructiveHint(plan.execTool)
	if a.planMode.Load() && isMCPExecutionTarget(plan.execTool, plan.permName) && !plannerTrustedMCP && (!plan.readOnly || !mcpServerAuthorized(plan.execTool) || mcpDestructiveHint(plan.execTool)) {
		reason := "writer/destructive target"
		if plan.readOnly && !mcpServerAuthorized(plan.execTool) {
			reason = "reader from an unauthorized server"
		}
		return toolOutcome{
			output:  fmt.Sprintf("blocked: MCP %s %q is unavailable during Plan mode; finish or exit Plan mode before requesting this call", reason, plan.permName),
			blocked: true,
			errMsg:  "blocked: MCP target is unavailable during planning",
		}, true
	}
	return toolOutcome{}, false
}

// commitResolvedSkip applies deferred proxy bookkeeping only after the
// validated call has passed tool.before. Discovery and decline actions then
// finish locally without entering permission, hook, lease, or Execute paths.
func (a *Agent) commitResolvedSkip(plan *toolCallPlan) (toolOutcome, bool) {
	if plan == nil || plan.resolvedMeta == nil {
		return toolOutcome{}, false
	}
	resolved := plan.resolved
	if resolved.Commit != nil {
		if err := resolved.Commit(); err != nil {
			return toolOutcome{output: fmt.Sprintf("error: %v", err), errMsg: firstLine(err.Error())}, true
		}
	}
	if resolved.SkipExecute {
		return a.resolvedSkipOutcome(plan, resolved), true
	}
	return toolOutcome{}, false
}

// proxyResolutionError preserves non-input resolver failures while diagnosing
// only the private, repairable envelope errors marked by the resolver.
func (a *Agent) proxyResolutionError(plan *toolCallPlan, err error) toolOutcome {
	var inputErr *capabilityInputError
	if errors.As(err, &inputErr) {
		return a.diagnoseCapabilityInputFailure(plan, err)
	}
	return toolOutcome{output: fmt.Sprintf("error: %v", err), errMsg: firstLine(err.Error())}
}

// applyRecoveryAndPermission applies write-scope and ordinary permission
// policy. Recovery review is informational and never participates in tool
// availability or execution decisions.
func (a *Agent) applyRecoveryAndPermission(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if blocked, early := a.applyWriteAccess(ctx, plan); early {
		return blocked, true
	}
	// Trusted MCP fast path: installed tools and authorized lifecycle connects
	// (mcp_connect__*) skip ordinary Ask/Auto/dontAsk gates. Only explicit denyand live authorization apply —
	// first connect of an installed server mustnot re-prompt under headless or partial-auto policies.
	gate := a.svc.gateSnapshot()
	trustedMCP := isInstalledMCPTool(plan.execTool) || isMCPLifecycleConnectTarget(plan.execTool)
	readOnlyPresetNeedsGate := a.svc.permissionPreset != nil && a.svc.permissionPreset() == "read-only" && !plan.readOnly
	if trustedMCP && !readOnlyPresetNeedsGate {
		if !mcpServerAuthorized(plan.execTool) {
			return toolOutcome{
				output:  "blocked: this project MCP server identity has not been authorized; approve the server from a parent session and retry",
				blocked: true,
				errMsg:  "blocked: MCP server identity is not authorized",
			}, true
		}
		if denyGate, ok := gate.(ExplicitDenyGate); ok && denyGate.ExplicitlyDenies(plan.permName, plan.permArgs) {
			return toolOutcome{
				output:  "blocked: denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain.",
				blocked: true,
				errMsg:  "blocked by permission policy",
			}, true
		}
	} else if gate != nil && !plan.skipOrdinaryGate {
		allow, reason, err := gate.Check(ctx, plan.permName, plan.permArgs, plan.readOnly)
		if err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: %s (%v)", reason, err),
				blocked: true,
				errMsg:  fmt.Sprintf("blocked: %v", err),
			}, true
		}
		// permission.decision: the host verdict is computed first; theextension rulingmayoverrideitineitherdirection
		// (an allowoverriding a host deny is the full-trust contract and is audited).
		if blocked, early := a.interceptExtensionPermission(ctx, plan, &allow); early {
			return blocked, true
		}
		if !allow {
			return toolOutcome{
				output:  "blocked: " + reason,
				blocked: true,
				errMsg:  "blocked by permission policy",
			}, true
		}
		// A write explicitly authorized while the session is read-only runs this
		// one call in the workspace sandbox. The session preset itself stays
		// read-only; session-scoped approval only reuses the same narrow grant.
		if !plan.readOnly && a.svc.permissionPreset != nil && a.svc.permissionPreset() == "read-only" {
			plan.permissionPreset = "workspace-write"
		}
	}
	return toolOutcome{}, false
}

// prepareToolExecution acquires write leases, parent write claims, runs
// PreToolUse hooks and preview checkpoints, and injects call context.
// Allofthishappensafterpermissionandbefore the concrete Execute call.
func (a *Agent) prepareToolExecution(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if blocked, early := a.prepareWriteCoordination(ctx, plan); early {
		return blocked, true
	}
	// Acquire the checkpoint barrier before preimage capture and any hook. It isheld through post hooks and
	// AfterMutation so rewind cannot interleave withwriter-side user code.
	if (plan.effects.WorkspaceMutation || hookNeedsWorkspaceWriteGuard(plan)) &&
		a.svc.mutationObserver != nil && a.svc.mutationObserver.Store() != nil {
		barrier := a.svc.mutationObserver.Store().Barrier()
		if err := barrier.EnterWrite(); err != nil {
			return toolOutcome{output: "blocked: " + err.Error(), blocked: true, errMsg: "blocked: mutation barrier unavailable"}, true
		}
		plan.releaseMutationWrite = barrier.ExitWrite
	}
	// Checkpoint the file this writer is about to change before PreToolUse.
	// A hook may mutate and then block the call, so the deferred
	// AfterMutationstillfinalizesthefingerprintonevery return path. Built-in
	// Previewers get precise paths (complete coverage). Bash / opaque
	// MCPwritersrecordexplicitcoveragegapsinstead of guessing targets.
	if plan.effects.WorkspaceMutation {
		a.observeBeforeMutation(ctx, plan)
		plan.mutationObserved = plan.mutationPath != ""
	}
	if hookNeedsWorkspaceWriteGuard(plan) && a.svc.mutationObserver != nil {
		a.svc.mutationObserver.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapHookWrite, Tool: plan.evidenceName, Detail: "tool hook may write paths that are not declared by the tool"})
	}
	// Proxy tools fire hooks against the real MCP target name and arguments.
	if a.svc.hooks != nil {
		if block, msg := a.svc.hooks.PreToolUse(ctx, plan.permName, plan.permArgs); block {
			if msg == "" {
				msg = "blocked by a PreToolUse hook"
			}
			return toolOutcome{
				output:  "blocked: " + msg,
				blocked: true,
				errMsg:  "blocked by PreToolUse hook",
			}, true
		}
	}
	cctx := tool.WithContextCompressor(withCallContext(ctx, plan.call.ID, a.svc.sink, a.svc.asker, a.planMode.Load()), a)
	if a.svc.interactionBroker != nil {
		cctx = mcpinteraction.WithBroker(cctx, a.svc.interactionBroker)
	}
	cctx, plan.mcpApp = tool.WithMCPAppCollector(cctx)
	cctx, plan.presentedFiles = tool.WithPresentedFilesCollector(cctx)
	cctx = WithSubagentDepth(cctx, a.subagentDepth)
	if a.task.ledger != nil {
		cctx = evidence.WithLedger(cctx, a.task.ledger)
	}
	if a.svc.jobs != nil {
		cctx = jobs.WithManager(cctx, a.svc.jobs)
	}
	if a.svc.sandboxEscape != nil {
		cctx = sandbox.WithEscapeApprover(cctx, a.svc.sandboxEscape)
	}
	if a.svc.configWrite != nil {
		cctx = tool.WithConfigWriteApprover(cctx, a.svc.configWrite)
	}
	if v := a.responseLanguage.Load(); v != nil {
		if lang, ok := v.(string); ok {
			cctx = WithResponseLanguagePreference(cctx, lang)
		}
	}
	if v := a.reasoningLanguage.Load(); v != nil {
		if lang, ok := v.(string); ok {
			cctx = WithReasoningLanguagePreference(cctx, lang)
		}
	}
	if a.svc.memQueue != nil {
		cctx = memory.WithQueue(cctx, a.svc.memQueue)
	}
	callID := plan.call.ID
	cctx = tool.WithProgress(cctx, func(chunk string) {
		a.svc.sink.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: callID, Output: chunk}})
	})
	plan.cctx = a.stampWriteRoots(cctx, plan)
	return toolOutcome{}, false
}

// finishToolExecution performs the concrete Execute, records evidence, runspost hooksandrecoveryobservation,
// and truncates the model-facing result.
func (a *Agent) finishToolExecution(ctx context.Context, plan *toolCallPlan) toolOutcome {
	if err := a.checkpointSession(ctx, CheckpointBeforeTopTool); err != nil {
		message := "session durability checkpoint failed before tool dispatch: " + err.Error()
		return toolOutcome{output: "error: " + message, errMsg: message, blocked: true}
	}
	plan.executed = true
	cctx := a.withWriteRecovery(plan.cctx, plan.call)
	if plan.expectedWriteSource.Path != "" {
		cctx = tool.WithExpectedWriteSource(cctx, plan.expectedWriteSource)
	}
	runTool := plan.runTool
	call := plan.call
	t := plan.tool
	readOnly := plan.readOnly
	permName := plan.permName
	permArgs := plan.permArgs
	var result string
	var images []string
	var err error
	// A call that was authorized under reader classification carries thatbasis into dispatch: the
	// MCPexecutionlayer re-verifies it linearizablyagainst server authorization and live safety metadata,
	// andrefuses topromote it into a writerlaneifreclassification landed after the gate.
	if readOnly && isInstalledMCPTool(runTool) && mcpServerAuthorized(runTool) && !mcpDestructiveHint(runTool) {
		cctx = tool.WithReaderExecutionIntent(cctx)
	}
	// Planner-trusted MCP: authorized + non-destructive, even withoutreadOnlyHint.
	// Finaldispatchre-checksliveauthorization/destructiveHint.
	if a.plannerMCPExecution && isMCPExecutionTarget(runTool, permName) && mcpServerAuthorized(runTool) && !mcpDestructiveHint(runTool) {
		cctx = tool.WithNonDestructiveMCPExecutionIntent(cctx)
	}
	if a.capabilityAudit != nil {
		cctx = tool.WithRemoteDispatchObserver(cctx, a.capabilityAudit.RecordRemoteDispatch)
	}
	plan.cctx = cctx
	var execution *tool.ShellExecution
	if plan.verification && a.svc.sink != nil {
		a.svc.sink.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: call.ID, Verifying: true}})
	}
	result, images, execution, err = a.dispatchResolvedTool(cctx, plan)
	// tool.after: extensions rule on the executed result (success or error)
	// before evidence, hooks, and recovery observation, so every downstreamconsumer sees the final
	// (possiblyreplaced) outcome.
	result, err = a.interceptToolAfter(ctx, call, result, err)
	// A tool that refused its own call never ran:
	// reportitlikethepermissionandplan-modeblocksaboveratherthanasanexecution failure.
	if msg, refused := tool.BlockedMessage(err); refused {
		return a.blockedToolOutcome(plan, msg)
	}
	// Track skill/capability outcomes for Delivery gates.
	a.noteCapabilityInvocation(call.Name, json.RawMessage(call.Arguments), err)
	// Success and failure hooks observe the result after the tool ran. Use thereal target name for proxiedtools.
	if a.svc.hooks != nil {
		if err != nil {
			a.svc.hooks.PostToolUseFailure(ctx, permName, permArgs, result, err)
		} else {
			a.svc.hooks.PostToolUse(ctx, permName, permArgs, result)
		}
	}
	// Always re-read after post hooks —
	// partialwritesandhooksideeffectscanchangethepreviewedpathevenwhentheconcrete tool returned an error.
	a.finalizeObservedToolReceipts(plan, result, execution, err)
	if err != nil {
		detail := result
		// Malformed-args failures are a transient model JSON glitch (e.g. optionswritten as ["a":"b"] →
		// "invalidcharacter ':' after array element"). Theargs can't be safely re-parsed,
		// butechoingthetool'sschemamakes theretry land valid insteadofrepeating the same broken shape.
		if !json.Valid([]byte(call.Arguments)) {
			detail = strings.TrimRight(detail, "\n") + "\nThe arguments were not valid JSON. Re-emit them exactly per this schema:\n" + string(t.Schema())
		}
		rawErr := fmt.Sprintf("error: %v\n%s", err, detail)
		body, truncMsg, original := a.boundProviderVisibleResult(rawErr, call.Name, call.ID)
		out := toolOutcome{
			runState: recoveryFailureState(err),
			output:   body, errMsg: firstLine(err.Error()), truncated: truncMsg != "" || original != "", truncMsg: truncMsg,
			execution: execution, mcpApp: toProviderMCPApp(plan.mcpApp), subagentOutcome: subagentOutcomeFromError(err),
		}
		var operationErr *tool.OperationError
		if errors.As(err, &operationErr) {
			d := operationErr.Diagnostic
			d.OperationID = call.ID
			out.diagnostic = &d
		}
		if original != "" {
			out.rawOutput = original
		}
		return out
	}
	// A foreground `task` sub-agent just finished — its result is the final answer.
	// (A backgrounded one returns a "Started…" string and stops later in a job, soit doesn't fire here.)
	// SubagentStop lets a hook react to delegated work.
	if a.svc.hooks != nil && call.Name == "task" && !isBackgroundTaskCall(call.Arguments) {
		a.svc.hooks.SubagentStop(ctx, result)
	}
	runState := outcomeRunState(toolOutcome{executed: true, output: result})
	var visionSummary *provider.VisionSummary
	if runState == provider.ToolRunCompleted {
		processed := a.processToolImages(cctx, result, images)
		result, visionSummary = processed.text, processed.summary
	}
	body, truncMsg, original := a.boundProviderVisibleResult(result, call.Name, call.ID)
	out := toolOutcome{
		runState: runState, output: body, images: images, visionSummary: visionSummary, truncated: truncMsg != "" || original != "", truncMsg: truncMsg,
		execution: execution, mcpApp: toProviderMCPApp(plan.mcpApp),
	}
	if plan.presentedFiles != nil {
		for _, file := range plan.presentedFiles() {
			out.presentedFiles = append(out.presentedFiles, provider.PresentedFile{Path: file.Path, Description: file.Description})
		}
	}
	if original != "" {
		out.rawOutput = original
	}
	return out
}

// observeBeforeMutation captures writer preimages and opaque-tool coverage gaps.
func (a *Agent) observeBeforeMutation(ctx context.Context, plan *toolCallPlan) {
	if a == nil || plan == nil {
		return
	}
	toolName := plan.evidenceName
	if toolName == "" {
		toolName = plan.call.Name
	}
	obs := a.svc.mutationObserver
	if obs != nil {
		if pv, ok := plan.execTool.(tool.Previewer); ok {
			if change, perr := pv.Preview(ctx, plan.execArgs); perr == nil && change.Path != "" {
				if evidence.ClassifyWriteScope(change.Path, a.writeWorkspaceRoot, a.scratchRoots()) == evidence.WriteScopeScratch {
					obs.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapScratch, Tool: toolName, Path: change.Path, Detail: "scratch path is not a project file"})
					plan.mutationPath = change.Path
					return
				}
				obs.BeforeMutationFromChange(change, toolName)
				plan.mutationPath = change.Path
				return
			}
		}
		// Non-previewable writers: record a coverage gap (do not guess paths).
		switch toolName {
		case "bash":
			obs.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapBashSideEffect, Tool: toolName, Detail: "bash side effects are not path-tracked"})
		default:
			// MCP or other writers without Previewer.
			if !plan.readOnly {
				obs.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapMCPExternal, Tool: toolName, Detail: "tool cannot describe local write paths"})
			}
		}
		return
	}
	// Legacy onPreEdit path.
	if a.svc.preEdit != nil {
		if pv, ok := plan.execTool.(tool.Previewer); ok {
			if change, perr := pv.Preview(ctx, plan.execArgs); perr == nil {
				a.svc.preEdit(change)
				plan.mutationPath = change.Path
			}
		}
	}
}

// observeAfterMutation records the after fingerprint when a concrete path wasknown before execution,
// regardless of tool success or failure.
func (a *Agent) observeAfterMutation(plan *toolCallPlan) bool {
	if a == nil || plan == nil || plan.mutationPath == "" || a.svc.mutationObserver == nil {
		return false
	}
	toolName := plan.evidenceName
	if toolName == "" {
		toolName = plan.call.Name
	}
	changed := a.svc.mutationObserver.AfterMutation(plan.mutationPath, toolName)
	if changed {
		plan.effects.StateMutation = true
		plan.effects.WorkspaceMutation = true
		plan.effects.ContentMutation = true
	}
	return changed
}
