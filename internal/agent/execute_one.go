package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/checkpoint"
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

// executeOne runs a single tool call. It is pure with respect to the event sink
// — the caller emits ToolDispatch/ToolResult — so it is safe to invoke from
// parallel goroutines. Stages: parse → policy → prepare → finish.
func (a *Agent) executeOne(ctx context.Context, turn *turnRuntime, call provider.ToolCall) (out toolOutcome) {
	ctx = a.withAgentContext(ctx)
	plan := &toolCallPlan{call: call}
	defer func() {
		if plan.mutationObserved && !plan.mutationAfterDone {
			a.observeAfterMutation(plan)
		}
		if plan.releaseMutationWrite != nil {
			plan.releaseMutationWrite()
		}
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
	defer finalizeWorkspaceMutationOutcome(&out, plan)

	if blocked, early := a.parseToolCall(ctx, plan); early {
		return blocked
	}
	// tool.before: extensions rule on the parsed call before any policy or
	// permission check. A valid replacement is re-parsed so every later stage
	// sees the call that will actually execute.
	if blocked, early := a.interceptToolBefore(ctx, plan); early {
		return blocked
	}
	if blocked, early := a.resolveToolPolicy(ctx, turn, plan); early {
		return blocked
	}
	if blocked, early := a.prepareToolExecution(ctx, plan); early {
		return blocked
	}
	return a.finishToolExecution(ctx, plan)
}

// parseToolCall resolves the canonical tool, rejects ambiguity/unknown tools,
// and applies repeat-success and stale-anchor guards.
func (a *Agent) parseToolCall(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	t, canonicalName, ambiguous := a.svc.tools.ResolveCall(plan.call.Name)
	if len(ambiguous) > 0 {
		msg := fmt.Sprintf("ambiguous MCP tool reference %q; use one of: %s", plan.call.Name, strings.Join(ambiguous, ", "))
		return toolOutcome{
			output: "error: " + msg,
			errMsg: msg,
		}, true
	}
	if t == nil {
		if server, ok := completedMCPConnect(a.svc.tools, plan.call.Name); ok {
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
	if out, blocked := a.repeatedFailureBlock(ctx, plan.call, t); blocked {
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
	if canonicalName == "bash" {
		var permissionReader bool
		plan.effects, permissionReader = evidence.ClassifyBashToolCall(plan.execArgs)
		if permissionReader {
			// Bash is schema-level writer-capable, but the host can resolve a
			// concrete invocation to read-only after parsing its arguments. Carry
			// that fact through permission, mutation accounting, evidence, and the
			// refreshed local tool receipt without changing the provider schema.
			plan.readOnly = true
			plan.resolvedMeta = &tool.ResolvedCall{TargetName: canonicalName, ReadOnly: true}
		}
	} else {
		plan.effects = evidence.ClassifyToolCall(plan.evidenceName, plan.evidenceArgs, plan.readOnly)
	}
	return toolOutcome{}, false
}

// resolveToolPolicy applies Plan mode, proxy resolution, delivery gates, Auto
// Guard, and permission checks. Permission must complete before any write lease.
func (a *Agent) resolveToolPolicy(ctx context.Context, turn *turnRuntime, plan *toolCallPlan) (toolOutcome, bool) {
	if blocked, early := a.applyPlanModeAndProxy(ctx, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyContextualToolGate(ctx, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyExecutionPreflight(turn, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyDeliveryPolicyGates(turn, plan); early {
		return blocked, true
	}
	// After proxy resolution, re-apply the batch mutation barrier using the
	// real target classification. Provider-visible proxies such as
	// use_capability advertise ReadOnly()==true before resolution and would
	// otherwise slip past the pre-run skip pass.
	if blocked, early := a.applyMutationDependencyBarrier(plan); early {
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
	case "update_goal":
		msg = "update_goal is only available while an active goal turn is running — no goal state was changed"
	case "complete_step":
		msg = "blocked: complete_step is only available after plan approval. While planning, keep task state with todo_write and present the plan for user approval."
	case "bash_output", "wait", "kill_shell":
		msg = "background jobs are not available in this context"
	}
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg)}, true
}

// applyMutationDependencyBarrier blocks later mutations and verifications in the
// same provider batch after an earlier modification failed. Host-proven
// read-only diagnosis (resolved ReadOnly with no verification classification)
// still runs.
func (a *Agent) applyMutationDependencyBarrier(plan *toolCallPlan) (toolOutcome, bool) {
	if a == nil || plan == nil {
		return toolOutcome{}, false
	}
	cause := a.mutationDependencyBarrier.Load()
	if cause == nil {
		return toolOutcome{}, false
	}
	verification := plan.evidenceName == "bash" && evidence.IsVerificationCommand(bashCommandFromArgs(plan.evidenceArgs))
	if !plan.effects.StateMutation && !verification {
		return toolOutcome{}, false
	}
	msg := cause.message()
	var ex *tool.ShellExecution
	// Structured shell metadata only for bash cards; other tools keep plain text.
	if plan.evidenceName == "bash" || plan.call.Name == "bash" {
		ex = shellPreflightExecution(plan, verification)
		if ex != nil {
			ex.FailurePhase = tool.ShellPhaseDependency
			ex.State = tool.ShellStateNotRun
			ex.MutationRisk = tool.ShellMutationNotStarted
			if verification {
				ex.Verification = tool.ShellVerificationNotRun
			}
		}
	}
	return toolOutcome{
		output:    msg,
		blocked:   true,
		errMsg:    firstLine(msg),
		execution: ex,
	}, true
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
		if outcome, blocked := contextualToolGateOutcome(ctx, plan.execTool, plan.permName); blocked {
			return outcome, true
		}
		plan.readOnly = rc.ReadOnly
		plan.classifyEffects()
		if outcome, blocked := a.readOnlyExecutionBlock(t, &rc); blocked {
			return blockedShellOutcome(outcome, plan), true
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
			if a.task.ledger != nil {
				// inspect/decline are not mutations; unavailable call targets are not success.
				success := !rc.Unavailable
				rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), success, true)
				a.task.ledger.Record(rec)
			}
			if rc.Unavailable {
				return toolOutcome{output: result, errMsg: firstLine(rc.UnavailableReason)}, true
			}
			body, truncMsg := truncateToolOutputFor(result, plan.call.Name, plan.call.ID)
			out := toolOutcome{output: body, truncated: truncMsg != "", truncMsg: truncMsg}
			if truncMsg != "" {
				out.rawOutput = result
			}
			return out, true
		}
	} else if outcome, blocked := a.readOnlyExecutionBlock(t, nil); blocked {
		return blockedShellOutcome(outcome, plan), true
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

// applyDeliveryPolicyGates enforces global deterministic shell contracts plus
// closed-loop-only criteria rules, and classifies mutation/verification.
func (a *Agent) applyDeliveryPolicyGates(turn *turnRuntime, plan *toolCallPlan) (toolOutcome, bool) {
	closedLoop := a.closedLoopActive()
	// Global deterministic shell contract (ordinary + closed loop). PowerShell
	// 5.1 &&/|| is enforced inside the bash tool itself so descriptor and error
	// text stay shell-accurate; the agent layers apply command-shape protections.
	// Closed-loop turns keep the broader classifier because a mutation
	// invalidates the verification receipt even when the exit status is honest.
	// Ordinary turns block only shapes where a later segment can actually hide
	// an earlier failure. Without that split, `go build ./... && go test ./...`,
	// `npm install && npm test`, and every other short-circuit chain would be
	// rejected for every user, though bash already reports the failing step's
	// status for them.
	if plan.evidenceName == "bash" {
		if evidence.BashToolCallMasksVerificationExit(plan.evidenceArgs) {
			msg := evidence.ShellContractPreflightMessage("mask_exit")
			if closedLoop {
				msg = "blocked: the trailing echo/printf of $? masks the verifier's exit status, so this command would look successful even when the check failed. Run the verifier or read-only extraction pipeline by itself and let its exit status be the tool result; for example: tail ... | head ... | node --check -"
			}
			return toolOutcome{
				output:    msg,
				blocked:   true,
				errMsg:    "blocked: verification exit status masked",
				execution: shellPreflightExecution(plan, true),
			}, true
		}
		mixed := evidence.BashToolCallMixesMutationAndMaskableVerification
		if closedLoop {
			mixed = evidence.BashToolCallMixesMutationAndVerification
		}
		if mixed(plan.evidenceArgs) {
			msg := evidence.ShellContractPreflightMessage("mixed")
			if closedLoop {
				msg = "blocked: this command mixes a verification check with a segment that may write state. Run the state-changing preparation separately while a todo is in_progress, then run a read-only verification command. For generated input, prefer a host-recognized read-only pipeline into the verifier (for example: tail ... | head ... | node --check -) instead of writing a temporary file."
			}
			return toolOutcome{
				output:    msg,
				blocked:   true,
				errMsg:    "blocked: mixed mutation and verification command",
				execution: shellPreflightExecution(plan, true),
			}, true
		}
		if evidence.BashToolCallUsesNonTerminalInlineInterpreter(plan.evidenceArgs) {
			msg := evidence.ShellContractPreflightMessage("inline_nonterminal")
			return toolOutcome{
				output:    msg,
				blocked:   true,
				errMsg:    "blocked: non-terminal inline interpreter command",
				execution: shellPreflightExecution(plan, false),
			}, true
		}
	}
	// Closed-loop only: any opaque inline interpreter is unauditable as evidence.
	if closedLoop && plan.evidenceName == "bash" && evidence.BashToolCallUsesOpaqueInlineInterpreter(plan.evidenceArgs) {
		return toolOutcome{
			output:    "blocked: closed-loop execution cannot audit inline interpreter source such as node -e or python -c, so executing it would become an opaque mutation and invalidate prior verification. For inspection, use read_file/grep or another host-proven read-only command. For validation, use a conventional verifier such as node --check, a project test/check/lint command, or a read-only extraction pipeline into the verifier. For an intentional state change, use a file tool or a script file under the current in_progress todo. " + evidence.VerificationCommandSummary(),
			blocked:   true,
			errMsg:    "blocked: opaque inline interpreter command",
			execution: shellPreflightExecution(plan, false),
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
	plan.verification = plan.evidenceName == "bash" && evidence.IsVerificationCommand(bashCommandFromArgs(plan.evidenceArgs))
	plan.planTransition, plan.planBefore, plan.planAfter, plan.planDiff = a.recoveryPlanTransition(plan.evidenceName, plan.evidenceArgs)
	episodeStopped := false
	if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
		plan.recoveryGen = ctrl.Generation()
		episodeStopped = ctrl.EpisodeStopped(a.recovery.taskID)
	}
	if a.svc.recoveryGate != nil && (plan.effects.StateMutation || plan.verification || plan.planTransition || episodeStopped) {
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
		dec, rerr := a.svc.recoveryGate.BeforeMutation(ctx, a.recoveryProposal(plan, episodeID, subject, preview))
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
	if blocked, early := a.applyWriteAccess(ctx, plan); early {
		return blocked, true
	}
	// Trusted MCP fast path: installed tools and authorized lifecycle connects
	// (mcp_connect__*) skip ordinary Ask/Auto/dontAsk gates. Only explicit deny
	// and live authorization apply — first connect of an installed server must
	// not re-prompt under headless or partial-auto policies.
	gate := a.svc.gateSnapshot()
	if isInstalledMCPTool(plan.execTool) || isMCPLifecycleConnectTarget(plan.execTool) {
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
		// permission.decision: the host verdict is computed first; the
		// extension ruling may override it in either direction (an allow
		// overriding a host deny is the full-trust contract and is audited).
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
	// Lazy workspace lease on the first real writer for every role setting.
	if plan.effects.WorkspaceMutation && a.svc.workspaceLease != nil {
		if err := a.svc.workspaceLease.AcquireWrite(ctx); err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: the workspace did not become available for writing: %v", err),
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
	if releaseParentWrite, perr := a.reserveParentWrite(plan.runTool, plan.runArgs, !plan.effects.WorkspaceMutation); perr != nil {
		return toolOutcome{
			output:  "blocked: " + perr.Error(),
			blocked: true,
			errMsg:  "blocked: write path claimed by background subagent",
		}, true
	} else if releaseParentWrite != nil {
		plan.releaseParentWrite = releaseParentWrite
	}
	// Acquire the checkpoint barrier before preimage capture and any hook. It is
	// held through post hooks and AfterMutation so rewind cannot interleave with
	// writer-side user code.
	if plan.effects.WorkspaceMutation && a.svc.mutationObserver != nil && a.svc.mutationObserver.Store() != nil {
		barrier := a.svc.mutationObserver.Store().Barrier()
		if err := barrier.EnterWrite(); err != nil {
			return toolOutcome{output: "blocked: " + err.Error(), blocked: true, errMsg: "blocked: mutation barrier unavailable"}, true
		}
		plan.releaseMutationWrite = barrier.ExitWrite
	}
	// Checkpoint the file this writer is about to change before PreToolUse.
	// A hook may mutate and then block the call, so the deferred AfterMutation
	// still finalizes the fingerprint on every return path. Built-in
	// Previewers get precise paths (complete coverage). Bash / opaque MCP
	// writers record explicit coverage gaps instead of guessing targets.
	if plan.effects.WorkspaceMutation {
		a.observeBeforeMutation(ctx, plan)
		plan.mutationObserved = plan.mutationPath != ""
		if toolHooksMayMutateWorkspace(a.svc.hooks) && a.svc.mutationObserver != nil {
			a.svc.mutationObserver.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapHookWrite, Tool: plan.evidenceName, Detail: "tool hook may write paths that are not declared by the tool"})
		}
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
	cctx = WithSubagentDepth(cctx, a.subagentDepth)
	if a.task.ledger != nil {
		cctx = evidence.WithLedger(cctx, a.task.ledger)
		cctx = evidence.WithSessionMessages(cctx, a.sess.conversation.Snapshot)
		if a.closedLoopActive() {
			cctx = evidence.WithClosedLoopExecution(cctx)
		}
	}
	if !a.planMode.Load() {
		cctx = a.withContractState(cctx)
	}
	if plan.planReplacementAuthorized {
		cctx = tool.WithPlanReplacementAuthorization(cctx)
	}
	if len(a.projectChecks) > 0 {
		cctx = instruction.WithChecks(cctx, a.projectChecks)
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

// finishToolExecution performs the concrete Execute, records evidence, runs
// post hooks and recovery observation, and truncates the model-facing result.
func (a *Agent) finishToolExecution(ctx context.Context, plan *toolCallPlan) toolOutcome {
	plan.executed = true
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
	mutates := plan.effects.StateMutation
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
	var execution *tool.ShellExecution
	if de, ok := runTool.(tool.DetailedExecutor); ok {
		var detailed tool.DetailedResult
		detailed, err = de.ExecuteDetailed(cctx, runArgs)
		result, images, execution = detailed.Output, detailed.Images, detailed.Execution
		// Annotate verification outcome when the host classified this call as a verifier.
		if execution != nil && plan.verification {
			switch {
			case err != nil:
				execution.Verification = tool.ShellVerificationFailed
			default:
				execution.Verification = tool.ShellVerificationPassed
			}
		} else if execution != nil && execution.Verification == "" {
			execution.Verification = tool.ShellVerificationNotVerification
		}
		// Sole opaque inline interpreters are allowed outside Delivery but cannot
		// prove mutation completeness.
		if execution != nil && evidence.BashCommandMayBeOpaqueMutation(runArgs) &&
			execution.MutationRisk == tool.ShellMutationMayHaveCompleted {
			execution.MutationRisk = tool.ShellMutationUnknown
		}
	} else if it, ok := runTool.(tool.ImageTool); ok {
		result, images, err = it.ExecuteWithImages(cctx, runArgs)
	} else {
		result, err = runTool.Execute(cctx, runArgs)
	}
	// tool.after: extensions rule on the executed result (success or error)
	// before evidence, hooks, and recovery observation, so every downstream
	// consumer sees the final (possibly replaced) outcome.
	result, err = a.interceptToolAfter(ctx, call, result, err)
	// A tool that refused its own call never ran: report it like the permission
	// and plan-mode blocks above rather than as an execution failure.
	if msg, refused := tool.BlockedMessage(err); refused {
		return a.blockedToolOutcome(plan, msg)
	}
	// Track skill/capability outcomes for Delivery gates.
	a.noteCapabilityInvocation(call.Name, json.RawMessage(call.Arguments), err)
	// Success and failure hooks observe the result after the tool ran. Use the
	// real target name for proxied tools.
	if a.svc.hooks != nil {
		if err != nil {
			a.svc.hooks.PostToolUseFailure(ctx, permName, permArgs, result, err)
		} else {
			a.svc.hooks.PostToolUse(ctx, permName, permArgs, result)
		}
	}
	// Always re-read after post hooks — partial writes and hook side effects can
	// change the previewed path even when the concrete tool returned an error.
	a.finalizeObservedToolReceipts(plan, result, execution, err)
	result = a.withRecoveryObservation(ctx, evidenceName, evidenceArgs, readOnly, mutates, result, err, recoveryGen)
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
		rawErr := fmt.Sprintf("error: %v\n%s", err, detail)
		body, truncMsg := truncateToolOutputFor(rawErr, call.Name, call.ID)
		out := toolOutcome{
			output: body, errMsg: firstLine(err.Error()), truncated: truncMsg != "", truncMsg: truncMsg,
			execution: execution, recoveryGeneration: recoveryGen,
		}
		if truncMsg != "" {
			out.rawOutput = rawErr
		}
		return out
	}
	if mutates {
		a.clearRepeatFailuresAfterMutation(evidenceName, evidenceArgs, readOnly)
	}
	a.recordRepeatSuccess(call, t)
	// A foreground `task` sub-agent just finished — its result is the final answer.
	// (A backgrounded one returns a "Started…" string and stops later in a job, so
	// it doesn't fire here.) SubagentStop lets a hook react to delegated work.
	if a.svc.hooks != nil && call.Name == "task" && !isBackgroundTaskCall(call.Arguments) {
		a.svc.hooks.SubagentStop(ctx, result)
	}
	body, truncMsg := truncateToolOutputFor(result, call.Name, call.ID)
	out := toolOutcome{
		output: body, images: images, truncated: truncMsg != "", truncMsg: truncMsg,
		execution: execution, recoveryGeneration: recoveryGen,
	}
	if truncMsg != "" {
		out.rawOutput = result
	}
	return out
}

// observeBeforeMutation captures preimages for Previewable writers and records
// explicit coverage gaps for bash / opaque MCP tools. Host-internal only.
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

// observeAfterMutation records the after fingerprint when a concrete path was
// known before execution, regardless of tool success or failure.
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
