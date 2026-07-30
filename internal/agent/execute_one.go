package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// toolCallPlan holds the resolved, policy-checked state for one tool call.
// Package-private; not shared across goroutines beyond the single executeOne
// invocation that owns it.
type toolCallPlan struct {
	call          provider.ToolCall
	tool          tool.Tool
	canonicalName string

	permName     string
	permArgs     json.RawMessage
	execTool     tool.Tool
	execArgs     json.RawMessage
	evidenceName string
	evidenceArgs json.RawMessage
	readOnly     bool

	resolved     tool.ResolvedCall
	resolvedMeta *tool.ResolvedCall

	mutates                   bool
	verification              bool
	planTransition            bool
	planBefore                string
	planAfter                 string
	planReplacementAuthorized bool
	recoveryGen               uint64

	runTool            tool.Tool
	runArgs            json.RawMessage
	cctx               context.Context
	releaseParentWrite func()
}

// executeOne runs a single tool call. It is pure with respect to the event sink
// — the caller emits ToolDispatch/ToolResult — so it is safe to invoke from
// parallel goroutines. Stages: parse → policy → prepare → finish.
func (a *Agent) executeOne(ctx context.Context, call provider.ToolCall) (out toolOutcome) {
	plan := &toolCallPlan{call: call}
	defer func() {
		if plan.releaseParentWrite != nil {
			plan.releaseParentWrite()
		}
		if plan.resolvedMeta == nil {
			return
		}
		out.resolved = true
		out.resolvedName = plan.resolvedMeta.TargetName
		out.capabilityID = plan.resolvedMeta.CapabilityID
		out.resolvedReadOnly = plan.resolvedMeta.ReadOnly
	}()

	if blocked, early := a.parseToolCall(plan); early {
		return blocked
	}
	if blocked, early := a.resolveToolPolicy(ctx, plan); early {
		return blocked
	}
	if blocked, early := a.prepareToolExecution(ctx, plan); early {
		return blocked
	}
	return a.finishToolExecution(ctx, plan)
}

// parseToolCall resolves the canonical tool, rejects ambiguity/unknown tools,
// and applies repeat-success and stale-anchor guards.
func (a *Agent) parseToolCall(plan *toolCallPlan) (toolOutcome, bool) {
	t, canonicalName, ambiguous := a.tools.ResolveCall(plan.call.Name)
	if len(ambiguous) > 0 {
		msg := fmt.Sprintf("ambiguous MCP tool reference %q; use one of: %s", plan.call.Name, strings.Join(ambiguous, ", "))
		return toolOutcome{
			output: "error: " + msg,
			errMsg: msg,
		}, true
	}
	if t == nil {
		if server, ok := completedMCPConnect(a.tools, plan.call.Name); ok {
			return toolOutcome{
				output: fmt.Sprintf("MCP server %q is connected; its real tools are now available", server),
			}, true
		}
		return toolOutcome{
			output: fmt.Sprintf("error: unknown tool %q", plan.call.Name),
			errMsg: fmt.Sprintf("unknown tool %q", plan.call.Name),
		}, true
	}
	if out, blocked := a.repeatedSuccessBlock(plan.call, t); blocked {
		return toolOutcome{
			output:  out,
			blocked: true,
			errMsg:  loopGuardBlockErrMsg,
		}, true
	}
	if out, blocked := a.repeatedFailureBlock(plan.call, t); blocked {
		return toolOutcome{
			output:  out,
			blocked: true,
			errMsg:  loopGuardBlockErrMsg,
		}, true
	}
	if out, blocked := a.staleAnchorEditBlock(plan.call); blocked {
		return toolOutcome{
			output:  out,
			blocked: true,
			errMsg:  "blocked: fresh read required",
		}, true
	}
	plan.tool = t
	plan.canonicalName = canonicalName
	plan.permName = canonicalName
	plan.permArgs = json.RawMessage(plan.call.Arguments)
	plan.execTool = t
	plan.execArgs = json.RawMessage(plan.call.Arguments)
	plan.evidenceName = canonicalName
	plan.evidenceArgs = json.RawMessage(plan.call.Arguments)
	plan.readOnly = t.ReadOnly()
	return toolOutcome{}, false
}

// resolveToolPolicy applies Plan mode, proxy resolution, delivery gates, Auto
// Guard, and permission checks. Permission must complete before any write lease.
func (a *Agent) resolveToolPolicy(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if blocked, early := a.applyPlanModeAndProxy(ctx, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyDeliveryPolicyGates(plan); early {
		return blocked, true
	}
	if blocked, early := a.applyRecoveryAndPermission(ctx, plan); early {
		return blocked, true
	}
	return toolOutcome{}, false
}

// applyPlanModeAndProxy handles initial Plan mode, proxy resolution / skip path,
// resolved-target Plan re-check, and MCP Plan availability.
func (a *Agent) applyPlanModeAndProxy(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	t := plan.tool
	call := plan.call
	if a.planMode.Load() {
		// Translate the tool's optional plan-mode self-report into the policy's
		// tri-state. Mirrors the t.(tool.Previewer) assertion precedent below.
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
	// Resolve proxy tools (use_capability) to the real MCP target before
	// permission, hooks, and evidence. Provider transcript keeps call.Name.
	if resolver, ok := t.(tool.CallResolver); ok {
		rc, rerr := resolver.ResolveCall(ctx, json.RawMessage(call.Arguments))
		if rerr != nil {
			return toolOutcome{
				output: fmt.Sprintf("error: %v", rerr),
				errMsg: firstLine(rerr.Error()),
			}, true
		}
		plan.resolved = rc
		plan.resolvedMeta = &plan.resolved
		if rc.TargetName != "" {
			plan.permName = rc.TargetName
			plan.evidenceName = rc.TargetName
		}
		if len(rc.Args) > 0 {
			plan.permArgs = rc.Args
			plan.evidenceArgs = rc.Args
			plan.execArgs = rc.Args
		}
		if rc.Target != nil {
			plan.execTool = rc.Target
		}
		plan.readOnly = rc.ReadOnly
		if outcome, blocked := a.readOnlyExecutionBlock(t, &rc); blocked {
			return outcome, true
		}
		if rc.Commit != nil {
			if err := rc.Commit(); err != nil {
				return toolOutcome{
					output: fmt.Sprintf("error: %v", err),
					errMsg: firstLine(err.Error()),
				}, true
			}
		}
		if rc.SkipExecute {
			// Resolution completed without target execution; still record a meta receipt.
			// A connected mcp-server call completes during resolution by listing
			// its live tools, so account for that successful call here too.
			if rc.ProxyAction == "call" && !rc.Unavailable {
				a.noteCapabilityInvocation(call.Name, json.RawMessage(call.Arguments), nil)
			}
			result := rc.Result
			if a.evidence != nil {
				// inspect/decline are not mutations; unavailable call targets are not success.
				success := !rc.Unavailable
				rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), success, true)
				a.evidence.Record(rec)
			}
			if rc.Unavailable {
				return toolOutcome{output: result, errMsg: firstLine(rc.UnavailableReason)}, true
			}
			body, truncMsg := truncateToolOutput(result)
			return toolOutcome{output: body, truncated: truncMsg != "", truncMsg: truncMsg}, true
		}
	} else if outcome, blocked := a.readOnlyExecutionBlock(t, nil); blocked {
		return outcome, true
	}

	// A proxy resolution can point at a target with an explicit planning-phase
	// opt-out even though the proxy itself has none. Re-check the resolved target
	// before its ordinary permission and sandbox path.
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

// applyDeliveryPolicyGates enforces delivery-profile bash and criteria rules and
// classifies whether the call mutates workspace state.
func (a *Agent) applyDeliveryPolicyGates(plan *toolCallPlan) (toolOutcome, bool) {
	if a.deliveryProfile && plan.evidenceName == "bash" && evidence.BashToolCallMasksVerificationExit(plan.evidenceArgs) {
		return toolOutcome{
			output:  "blocked: the trailing echo/printf of $? masks the verifier's exit status, so this command would look successful even when the check failed. Run the verifier or read-only extraction pipeline by itself and let its exit status be the tool result; for example: tail ... | head ... | node --check -",
			blocked: true,
			errMsg:  "blocked: verification exit status masked",
		}, true
	}
	if a.deliveryProfile && plan.evidenceName == "bash" && evidence.BashToolCallMixesMutationAndVerification(plan.evidenceArgs) {
		return toolOutcome{
			output:  "blocked: this command mixes a verification check with a segment that may write state. Run the state-changing preparation separately while a todo is in_progress, then run a read-only verification command. For generated input, prefer a host-recognized read-only pipeline into the verifier (for example: tail ... | head ... | node --check -) instead of writing a temporary file.",
			blocked: true,
			errMsg:  "blocked: mixed mutation and verification command",
		}, true
	}
	if a.deliveryProfile && plan.evidenceName == "bash" && evidence.BashToolCallUsesOpaqueInlineInterpreter(plan.evidenceArgs) {
		return toolOutcome{
			output:  "blocked: delivery mode cannot audit inline interpreter source such as node -e or python -c, so executing it would become an opaque mutation and invalidate prior verification. For inspection, use read_file/grep or another host-proven read-only command. For validation, use a conventional verifier such as node --check, a project test/check/lint command, or a read-only extraction pipeline into the verifier. For an intentional state change, use a file tool or a script file under the current in_progress todo.",
			blocked: true,
			errMsg:  "blocked: opaque inline interpreter command",
		}, true
	}

	plan.mutates = evidence.ToolCallMutates(plan.evidenceName, plan.evidenceArgs, plan.readOnly)
	if a.deliveryProfile && evidence.ToolCallRequiresDeliveryCriteria(plan.evidenceName, plan.evidenceArgs, plan.readOnly) && !a.deliveryCriteriaEstablished {
		return toolOutcome{
			output:  "blocked: delivery-first mode requires acceptance criteria before state-changing work. Call todo_write with a concrete, verifiable task list, then retry this tool call.",
			blocked: true,
			errMsg:  "blocked: delivery acceptance criteria required",
		}, true
	}
	if a.deliveryProfile && plan.mutates && !a.hasActiveCanonicalTodo() {
		return toolOutcome{
			output:  "blocked: delivery-first mode requires every state change to belong to the current in_progress todo. Preserve the completed todo prefix, append a concrete new item if more work was discovered, mark that item in_progress with todo_write, then retry this mutation.",
			blocked: true,
			errMsg:  "blocked: active delivery todo required",
		}, true
	}
	return toolOutcome{}, false
}

// applyRecoveryAndPermission runs Auto Guard then ordinary permission. Neither
// acquires a write lease; that happens only after permission in prepare.
func (a *Agent) applyRecoveryAndPermission(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	// Auto Guard: after resolution/mutation classification, before
	// permission approval and workspace write-lock acquisition, so a waiting
	// recovery card never holds a write lease. Consult on mutations,
	// verification, plan transitions, and again for every tool once an Episode
	// is exhausted so host-proven read-only diagnosis can remain available while
	// further execution is quarantined. Ask/Yolo still bypass inside the gate.
	plan.verification = plan.evidenceName == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(plan.evidenceArgs))
	plan.planTransition, plan.planBefore, plan.planAfter = a.recoveryPlanTransition(plan.evidenceName, plan.evidenceArgs)
	episodeStopped := false
	if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
		plan.recoveryGen = ctrl.Generation()
		episodeStopped = ctrl.EpisodeStopped(a.recoveryTaskID)
	}
	if a.recoveryGate != nil && (plan.mutates || plan.verification || plan.planTransition || episodeStopped) {
		subject := recoverySubject(plan.evidenceName, plan.evidenceArgs)
		if plan.planTransition {
			subject = "Update the active execution plan"
		}
		preview := strings.TrimSpace(plan.call.Diff)
		if preview == "" {
			preview = subject
		}
		if plan.planTransition {
			preview = plan.planAfter
		}
		episodeID := ""
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			episodeID = ctrl.EpisodeID()
		}
		dec, rerr := a.recoveryGate.BeforeMutation(ctx, RecoveryProposal{
			AgentID:        a.recoveryAgentID,
			TaskID:         a.recoveryTaskID,
			TaskScopeID:    recoveryTaskScopeID(a.deliveryScopeID, a.recoveryRunSeq.Load()),
			EpisodeID:      episodeID,
			TaskSummary:    a.recoveryTaskSummary,
			Tool:           plan.evidenceName,
			Args:           plan.evidenceArgs,
			Subject:        subject,
			Preview:        preview,
			ReadOnly:       plan.readOnly,
			Mutates:        plan.mutates,
			Verification:   plan.verification,
			PlanTransition: plan.planTransition,
			PlanBefore:     plan.planBefore,
			PlanAfter:      plan.planAfter,
		})
		if dec.Generation != 0 {
			plan.recoveryGen = dec.Generation
		}
		if rerr != nil && !dec.Blocked {
			return toolOutcome{
				output:             fmt.Sprintf("blocked: Auto Guard error: %v", rerr),
				blocked:            true,
				errMsg:             "blocked: Auto Guard error",
				recoveryGeneration: plan.recoveryGen,
			}, true
		}
		if dec.Blocked || !dec.Allow {
			msg := strings.TrimSpace(dec.Message)
			if msg == "" {
				msg = "blocked: Auto Guard declined this mutation"
			}
			if !strings.HasPrefix(msg, "blocked:") {
				msg = "blocked: " + msg
			}
			return toolOutcome{
				output:  msg,
				blocked: true,
				// Surface the concrete stopped operation and next step in the
				// failed tool card instead of exposing only an internal guard name.
				errMsg:             firstLine(msg),
				recoveryGeneration: plan.recoveryGen,
				recoveryStopTurn:   dec.StopTurn,
				recoveryStopReason: dec.StopReason,
			}, true
		}
		plan.planReplacementAuthorized = plan.planTransition && dec.AuthorizePlanReplacement
	}
	// Trusted MCP fast path: installed tools and authorized lifecycle connects
	// (mcp_connect__*) skip ordinary Ask/Auto/dontAsk gates. Only explicit deny
	// and live authorization apply — first connect of an installed server must
	// not re-prompt under headless or partial-auto policies.
	if isInstalledMCPTool(plan.execTool) || isMCPLifecycleConnectTarget(plan.execTool) {
		if !mcpServerAuthorized(plan.execTool) {
			return toolOutcome{
				output:  "blocked: this project MCP server identity has not been authorized; approve the server from a parent session and retry",
				blocked: true,
				errMsg:  "blocked: MCP server identity is not authorized",
			}, true
		}
		if denyGate, ok := a.gate.(ExplicitDenyGate); ok && denyGate.ExplicitlyDenies(plan.permName, plan.permArgs) {
			return toolOutcome{
				output:  "blocked: denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain.",
				blocked: true,
				errMsg:  "blocked by permission policy",
			}, true
		}
	} else if a.gate != nil {
		allow, reason, err := a.gate.Check(ctx, plan.permName, plan.permArgs, plan.readOnly)
		if err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: %s (%v)", reason, err),
				blocked: true,
				errMsg:  fmt.Sprintf("blocked: %v", err),
			}, true
		}
		if !allow {
			return toolOutcome{
				output:  "blocked: " + reason,
				blocked: true,
				errMsg:  "blocked by permission policy",
			}, true
		}
	}
	return toolOutcome{}, false
}

// prepareToolExecution acquires write leases, parent write claims, runs
// PreToolUse hooks and preview checkpoints, and injects call context. All of
// this happens after permission and before the concrete Execute call.
func (a *Agent) prepareToolExecution(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	// Acquire after permission is granted but before PreToolUse: hooks are user
	// shell code and can themselves change the workspace. This keeps readers
	// concurrent and avoids holding the workspace during an approval prompt while
	// still covering every write-side action that follows authorization.
	if a.deliveryProfile && plan.mutates && a.workspaceLease != nil {
		if err := a.workspaceLease.AcquireWrite(ctx); err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: the workspace did not become available for Delivery writing: %v", err),
				blocked: true,
				errMsg:  "blocked: workspace write lease unavailable",
			}, true
		}
	}
	// Resolve the concrete execution target before hooks. A proxy may carry a
	// different target/name/argument set than the provider-visible call.
	plan.runTool = plan.execTool
	plan.runArgs = plan.execArgs
	if plan.resolved.Target != nil {
		plan.runTool = plan.resolved.Target
		plan.runArgs = plan.resolved.Args
		if len(plan.runArgs) == 0 {
			plan.runArgs = json.RawMessage(`{}`)
		}
	}
	// Hold the parent claim before PreToolUse: hooks are user shell code and may
	// mutate the same workspace. The reservation remains live through hooks,
	// checkpointing, and the concrete Execute call, closing both hook-side and
	// check-before-write TOCTOU windows. Dynamic Economy/MCP tools are covered
	// here after registry lookup without schema-changing wrappers.
	// executeOne defers plan.releaseParentWrite so every return path releases.
	if releaseParentWrite, perr := a.reserveParentWrite(plan.runTool, plan.runArgs, plan.readOnly); perr != nil {
		return toolOutcome{
			output:  "blocked: " + perr.Error(),
			blocked: true,
			errMsg:  "blocked: write path claimed by background subagent",
		}, true
	} else if releaseParentWrite != nil {
		plan.releaseParentWrite = releaseParentWrite
	}
	// PreToolUse hooks run after permission is granted but before the call: a
	// gating hook (exit 2) refuses it, surfaced to the model like a gate denial.
	// Proxy tools fire hooks against the real MCP target name and arguments.
	if a.hooks != nil {
		if block, msg := a.hooks.PreToolUse(ctx, plan.permName, plan.permArgs); block {
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
	// Checkpoint the file this writer is about to change, so the turn can be
	// rewound. Fires after all gating (the edit is cleared to run) and only for
	// tools that can describe their change; a Preview error means the edit will
	// likely fail anyway, so we skip rather than snapshot a stale state.
	if a.onPreEdit != nil && !plan.readOnly {
		if pv, ok := plan.execTool.(tool.Previewer); ok {
			if change, perr := pv.Preview(plan.execArgs); perr == nil {
				a.onPreEdit(change)
			}
		}
	}
	cctx := withCallContext(ctx, plan.call.ID, a.sink, a.asker, a.planMode.Load())
	cctx = WithSubagentDepth(cctx, a.subagentDepth)
	if a.evidence != nil {
		cctx = evidence.WithLedger(cctx, a.evidence)
		cctx = evidence.WithSessionMessages(cctx, a.session.Snapshot())
		if a.deliveryProfile {
			cctx = evidence.WithDeliveryProfile(cctx)
		}
	}
	if !a.planMode.Load() {
		cctx = evidence.WithTodoState(cctx, a.CanonicalTodoState())
	}
	if plan.planReplacementAuthorized {
		cctx = tool.WithPlanReplacementAuthorization(cctx)
	}
	if len(a.projectChecks) > 0 {
		cctx = instruction.WithChecks(cctx, a.projectChecks)
	}
	if a.jobs != nil {
		cctx = jobs.WithManager(cctx, a.jobs)
	}
	if a.sandboxEscapeApprover != nil {
		cctx = sandbox.WithEscapeApprover(cctx, a.sandboxEscapeApprover)
	}
	if a.configWriteApprover != nil {
		cctx = tool.WithConfigWriteApprover(cctx, a.configWriteApprover)
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
	if a.memQueue != nil {
		cctx = memory.WithQueue(cctx, a.memQueue)
	}
	callID := plan.call.ID
	cctx = tool.WithProgress(cctx, func(chunk string) {
		a.sink.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: callID, Output: chunk}})
	})
	plan.cctx = cctx
	return toolOutcome{}, false
}

// finishToolExecution performs the concrete Execute, records evidence, runs
// post hooks and recovery observation, and truncates the model-facing result.
func (a *Agent) finishToolExecution(ctx context.Context, plan *toolCallPlan) toolOutcome {
	cctx := plan.cctx
	runTool := plan.runTool
	runArgs := plan.runArgs
	call := plan.call
	t := plan.tool
	readOnly := plan.readOnly
	permName := plan.permName
	permArgs := plan.permArgs
	evidenceName := plan.evidenceName
	evidenceArgs := plan.evidenceArgs
	mutates := plan.mutates
	recoveryGen := plan.recoveryGen

	var result string
	var images []string
	var err error
	// A call that was authorized under reader classification carries that
	// basis into dispatch: the MCP execution layer re-verifies it linearizably
	// against server authorization and live safety metadata, and refuses to
	// promote it into a writer lane if reclassification landed after the gate.
	if readOnly && isInstalledMCPTool(runTool) && mcpServerAuthorized(runTool) && !mcpDestructiveHint(runTool) {
		cctx = tool.WithReaderExecutionIntent(cctx)
	}
	// Planner-trusted MCP: authorized + non-destructive, even without
	// readOnlyHint. Final dispatch re-checks live authorization/destructiveHint.
	if a.plannerMCPExecution && isMCPExecutionTarget(runTool, permName) && mcpServerAuthorized(runTool) && !mcpDestructiveHint(runTool) {
		cctx = tool.WithNonDestructiveMCPExecutionIntent(cctx)
	}
	if it, ok := runTool.(tool.ImageTool); ok {
		result, images, err = it.ExecuteWithImages(cctx, runArgs)
	} else {
		result, err = runTool.Execute(cctx, runArgs)
	}
	if a.evidence != nil {
		// Always record the model-visible call for audit, then the real target
		// attributes for mutation/read classification when they differ.
		if call.Name == "complete_step" {
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
			a.evidence.Record(rec)
			if err == nil {
				a.advanceCanonicalTodo(rec.Step)
			}
		} else if evidenceName != call.Name {
			// Proxy: meta receipt (non-mutation) + real target receipt.
			a.evidence.Record(evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, true))
			rec := evidence.ReceiptFromToolCall(evidenceName, evidenceArgs, err == nil, readOnly)
			rec.OutputBytes = len(strings.TrimSpace(result))
			a.evidence.Record(rec)
		} else {
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
			rec.OutputBytes = len(strings.TrimSpace(result))
			a.evidence.Record(rec)
			if err == nil && call.Name == "todo_write" {
				a.setTodoState(rec.Todos)
				if len(rec.Todos) > 0 {
					a.deliveryCriteriaEstablished = true
				}
			}
		}
	}
	// Track skill/capability outcomes for Delivery gates.
	a.noteCapabilityInvocation(call.Name, json.RawMessage(call.Arguments), err)
	// Success and failure hooks observe the result after the tool ran. Use the
	// real target name for proxied tools.
	if a.hooks != nil {
		if err != nil {
			a.hooks.PostToolUseFailure(ctx, permName, permArgs, result, err)
		} else {
			a.hooks.PostToolUse(ctx, permName, permArgs, result)
		}
	}
	if a.recoveryGate != nil {
		a.observeRecoveryResult(ctx, evidenceName, evidenceArgs, readOnly, mutates, result, err, false, false, recoveryGen)
	}
	if err != nil {
		detail := result
		// Malformed-args failures are a transient model JSON glitch (e.g. options
		// written as ["a":"b"] → "invalid character ':' after array element"). The
		// args can't be safely re-parsed, but echoing the tool's schema makes the
		// retry land valid instead of repeating the same broken shape.
		if !json.Valid([]byte(call.Arguments)) {
			detail = strings.TrimRight(detail, "\n") + "\nThe arguments were not valid JSON. Re-emit them exactly per this schema:\n" + string(t.Schema())
		}
		a.recordRepeatFailure(call, t, err)
		body, truncMsg := truncateToolOutput(fmt.Sprintf("error: %v\n%s", err, detail))
		return toolOutcome{output: body, errMsg: firstLine(err.Error()), truncated: truncMsg != "", truncMsg: truncMsg, recoveryGeneration: recoveryGen}
	}
	if mutates {
		a.clearRepeatFailuresAfterMutation(evidenceName, evidenceArgs, readOnly)
	}
	a.recordRepeatSuccess(call, t)
	// A foreground `task` sub-agent just finished — its result is the final answer.
	// (A backgrounded one returns a "Started…" string and stops later in a job, so
	// it doesn't fire here.) SubagentStop lets a hook react to delegated work.
	if a.hooks != nil && call.Name == "task" && !isBackgroundTaskCall(call.Arguments) {
		a.hooks.SubagentStop(ctx, result)
	}
	body, truncMsg := truncateToolOutput(result)
	return toolOutcome{output: body, images: images, truncated: truncMsg != "", truncMsg: truncMsg, recoveryGeneration: recoveryGen}
}
