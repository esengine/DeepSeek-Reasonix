package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/workspacelease"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/permission"
)

// CodeMCPToolDisabled identifies a call refused by an explicit disabled_tools
// entry. The tool is absent from the registry, so this code keeps the reason
// visible when the model calls it from stale context.
const CodeMCPToolDisabled = tool.CodeMCPToolDisabled

// toolCallPlan holds the resolved, policy-checked state for one tool call.
// Package-private; not shared across goroutines beyond the single executeOne
// invocation that owns it.
type toolCallPlan struct {
	preview       *event.FileDiff
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

	admission *planmode.State

	mutates                   bool
	verification              bool
	planTransition            bool
	planBefore                string
	planAfter                 string
	planDiff                  string
	planReplacementAuthorized bool
	recoveryGen               uint64

	runTool              tool.Tool
	runArgs              json.RawMessage
	cctx                 context.Context
	releaseParentWrite   func()
	releaseMutationWrite func()
	leasePreparation     *workspacelease.PreparedCall

	// pathsBefore is the state of the turn's known paths taken before an
	// unclassifiable call ran, so its receipt can say what it actually touched.
	pathsBefore pathSnapshot
	// scanBefore is the whole workspace before the same call, which is what
	// lets an unclassifiable one be settled as having changed nothing.
	scanBefore workspaceScan

	// mutationPath is set when a Previewer described a concrete workspace path
	// for AfterMutation fingerprint capture (success or failure).
	// mutationWitness is the same preview's answer to a different question:
	// which lines a later output has to carry to have shown this change.
	mutationWitness   []string
	mutationPath      string
	declaredPaths     []string
	criteriaRewritten []string
	mutationObserved  bool
	mutationAfterDone bool
	// ranClean: the tool's own Execute returned no error, before tool.after ruled.
	ranClean           bool
	executed           bool
	hookMayHaveMutated bool
	// spawned collects the external origins of calls this tool made on its own
	// account, nested or through a sub-agent; it labels a result the tool
	// declares nothing external for.
	spawned *tool.ProvenanceSink
}

// provenanceOf is where the result's content came from: the tool's own
// answer, else what the calls it spawned fetched.
func (p *toolCallPlan) provenanceOf(t tool.Tool, args json.RawMessage) tool.Provenance {
	own := tool.ProvenanceOf(t, args)
	if own.External() || p.spawned == nil {
		return own
	}
	if inherited := p.spawned.Origin(); inherited.External() {
		return inherited
	}
	return own
}

// executeOne publishes resource invalidation on every exit, independently of
// its caller's ToolDispatch/ToolResult ordering.
func (a *Agent) executeOne(ctx context.Context, turn *turnRuntime, call provider.ToolCall) (out toolOutcome) {
	ctx = a.withAgentContext(ctx)
	plan := &toolCallPlan{call: call}
	defer func() { recordWorkspaceMutation(a.svc.sink, out.workspaceMutation) }()
	ctx = tool.WithInvoker(ctx, &nestedInvoker{a: a, turn: turn, parent: plan})
	todosBefore := a.todoStateBefore(call)
	defer func() { out.todoEcho = a.todoWriteEchoes(plan.call, todosBefore, out.errMsg) }()
	defer func() {
		if plan.mutationObserved && !plan.mutationAfterDone {
			out.checkpointErr = a.observeAfterMutation(plan)
		}
		if plan.releaseMutationWrite != nil {
			plan.releaseMutationWrite()
		}
		if err := plan.leasePreparation.Close(context.WithoutCancel(ctx)); err != nil {
			out.output += "\nwarning: " + err.Error()
			if out.errMsg == "" {
				out.errMsg = err.Error()
			} else {
				out.errMsg += "; " + err.Error()
			}
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
		out.resolvedProfile = delegationProfile(plan.resolvedMeta.Target, plan.resolvedMeta.Args)
	}()
	defer finalizeWorkspaceMutationOutcome(&out, plan)
	defer func() { out.preview = plan.preview }()

	if blocked, early := a.parseToolCall(ctx, plan); early {
		return blocked
	}
	// tool.before: extensions rule on the parsed call before any policy or
	// permission check; a valid replacement is re-parsed for every later stage.
	a.noteOriginMatch(plan)
	if blocked, early := a.interceptToolBefore(ctx, plan); early {
		return blocked
	}
	if blocked, early := a.rejectStaleAuthority(ctx); early {
		return blocked
	}
	// Read after tool.before: an extension may have substituted the call, and the
	// batch scan judged the one the model wrote.
	defer func() {
		out.endsRound = out.endsRound || tool.IsDecisionBarrierCall(plan.tool, json.RawMessage(plan.call.ExecutionArguments())) || tool.IsDecisionBarrier(plan.execTool)
	}()
	if blocked, early := a.refuseNetworkToolPaths(plan); early {
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
	plan.call = a.prepareCallArguments(plan.call)
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
		if refusal, ok := a.svc.tools.CallRefusal(plan.call.Name); ok {
			msg := refusal.String()
			return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg), refusalCode: refusal.Code}, true
		}
		if a.svc.postureLocked.Load() {
			msg := fmt.Sprintf("blocked: tool %q is not available: this run is read-only and offers only %s. Nothing else can be enabled from inside the run.", plan.call.Name, strings.Join(a.svc.tools.AllNames(), ", "))
			return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg), refusalCode: CodePostureToolNotAllowed}, true
		}
		return toolOutcome{
			output: fmt.Sprintf("error: unknown tool %q", plan.call.Name),
			errMsg: fmt.Sprintf("unknown tool %q", plan.call.Name),
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
	plan.permArgs = json.RawMessage(plan.call.ExecutionArguments())
	plan.execTool = t
	plan.execArgs = json.RawMessage(plan.call.ExecutionArguments())
	plan.evidenceName = canonicalName
	plan.evidenceArgs = json.RawMessage(plan.call.ExecutionArguments())
	plan.readOnly = t.ReadOnly()
	if canonicalName == "bash" && permission.BashCommandIsReadOnly(plan.execArgs) {
		// Bash is schema-level writer-capable, but the host can resolve a
		// concrete invocation to read-only after parsing its arguments. Carry
		// that fact through permission, mutation accounting, evidence, and the
		// refreshed local tool receipt without changing the provider schema.
		plan.readOnly = true
		plan.resolvedMeta = &tool.ResolvedCall{TargetName: canonicalName, ReadOnly: true}
	}
	return toolOutcome{}, false
}

// resolveToolPolicy applies Plan mode, proxy resolution, delivery gates, Auto
// Guard, and permission checks. Permission must complete before any write lease.
func (a *Agent) resolveToolPolicy(ctx context.Context, turn *turnRuntime, plan *toolCallPlan) (toolOutcome, bool) {
	if blocked, early := a.applyPlanModeAndProxy(ctx, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyCallContract(plan); early {
		return blocked, true
	}
	if blocked, early := a.applyContextualToolGate(ctx, plan); early {
		return blocked, true
	}
	if blocked, early := a.applyTargetAccess(ctx, plan); early {
		return blocked, true
	}
	a.previewForApproval(ctx, plan)
	if blocked, early := a.applyDeliveryPolicyGates(ctx, turn, plan); early {
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
	refusal := unavailableReason(ctx, target, name)
	msg := refusal.String()
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg), refusalCode: refusal.Code}, true
}

// applyMutationDependencyBarrier blocks later mutations and verifications in the
// same provider batch after an earlier modification failed. Host-proven
// read-only diagnosis (resolved ReadOnly with no verification classification)
// still runs.
func (a *Agent) applyMutationDependencyBarrier(plan *toolCallPlan) (toolOutcome, bool) {
	if a == nil || plan == nil {
		return toolOutcome{}, false
	}
	level := barrierLevel(a.mutationDependencyBarrier.Load())
	if level == barrierNone {
		return toolOutcome{}, false
	}
	verification := plan.evidenceName == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(plan.evidenceArgs))
	// Prefer the post-gate mutates flag; fall back to !readOnly so a resolved
	// writer proxy cannot claim non-mutation by skipping ToolCallMutates.
	mutates := plan.mutates || !plan.readOnly
	if !level.covers(mutates, verification) {
		return toolOutcome{}, false
	}
	msg := level.message()
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
	if a.PlanningPhase() {
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
		if decision := a.planModeDecision(t, plan.canonicalName, plan.readOnly, safety, json.RawMessage(call.ExecutionArguments())); decision.Blocked {
			return toolOutcome{
				output:  decision.Message,
				blocked: true,
				errMsg:  planPhaseBlockReason(decision),
			}, true
		}
		plan.admitPlanPhase(a)
	}
	// Resolve proxy tools (use_capability) to the real MCP target before
	// permission, hooks, and evidence. Provider transcript keeps call.Name.
	if resolver, ok := t.(tool.CallResolver); ok {
		rc, rerr := resolver.ResolveCall(ctx, json.RawMessage(call.ExecutionArguments()))
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
		if outcome, blocked := a.typedUnavailableOutcome(rc, call.Name, json.RawMessage(call.ExecutionArguments())); blocked {
			return outcome, true
		}
		if outcome, blocked := contextualToolGateOutcome(ctx, plan.execTool, plan.permName); blocked {
			return outcome, true
		}
		plan.readOnly = rc.ReadOnly
		if outcome, blocked := a.readOnlyExecutionBlock(t, &rc); blocked {
			return outcome, true
		}
		if outcome, blocked := a.planPhaseGateForTarget(plan); blocked {
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
				a.noteCapabilityInvocation(plan.canonicalName, json.RawMessage(call.ExecutionArguments()), nil, nil)
			}
			result := rc.Result
			if a.task.ledger != nil {
				// inspect/decline are not mutations; unavailable call targets are not success.
				success := !rc.Unavailable
				rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.ExecutionArguments()), success, evidence.ToolFacts{ReadOnly: true})
				a.task.ledger.Record(rec)
			}
			if rc.Unavailable {
				return toolOutcome{
					output:      result,
					errMsg:      firstLine(rc.UnavailableReason),
					blocked:     rc.RefusalCode != "",
					refusalCode: rc.RefusalCode,
				}, true
			}
			body, bound, truncMsg := a.boundToolOutput(result, plan.call.Name, plan.call.ID, plan.call.ExecutionArguments(), false)
			out := toolOutcome{output: body, bound: bound, truncMsg: truncMsg}
			out.keepFull(result)
			return out, true
		}
	} else if outcome, blocked := a.readOnlyExecutionBlock(t, nil); blocked {
		return outcome, true
	}

	if outcome, blocked := a.planPhaseGateForTarget(plan); blocked {
		return outcome, true
	}
	return toolOutcome{}, false
}

// applyDeliveryPolicyGates enforces global deterministic shell contracts plus
// delivery-profile-only criteria rules, and classifies mutation/verification.
func (a *Agent) applyDeliveryPolicyGates(ctx context.Context, turn *turnRuntime, plan *toolCallPlan) (toolOutcome, bool) {
	if outcome, blocked := a.applyShellShapeGates(ctx, plan); blocked {
		return outcome, true
	}
	plan.mutates = evidence.ToolCallMutates(plan.evidenceName, plan.evidenceArgs, plan.readOnly)
	persistentWorkflowCall := plan.evidenceName == "remember"
	if a.deliveryProfile && !persistentWorkflowCall && evidence.ToolCallRequiresDeliveryCriteria(plan.evidenceName, plan.evidenceArgs, plan.readOnly) && !turn.deliveryCriteriaEstablished {
		return toolOutcome{
			output:  "blocked: delivery-first mode requires acceptance criteria before state-changing work. Call todo_write with a concrete, verifiable task list, then retry this tool call.",
			blocked: true,
			errMsg:  "blocked: delivery acceptance criteria required",
		}, true
	}
	if a.deliveryProfile && !persistentWorkflowCall && plan.mutates && !a.hasActiveCanonicalTodo() {
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
	// verification, and plan transitions. Ask/Yolo still bypass inside the gate.
	plan.verification = plan.evidenceName == "bash" &&
		(bashDeclaredCheck(plan.evidenceArgs) != "" || evidence.CommandRunsVerification(bashCommandFromArgs(plan.evidenceArgs)))
	plan.planTransition, plan.planBefore, plan.planAfter, plan.planDiff = a.recoveryPlanTransition(plan.evidenceName, plan.evidenceArgs)
	if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
		plan.recoveryGen = ctrl.Generation()
	}
	if a.svc.recoveryGate != nil && (plan.mutates || plan.verification || plan.planTransition) {
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
				output:  fmt.Sprintf("blocked: Auto Guard error: %v", rerr),
				blocked: true,
				errMsg:  "blocked: Auto Guard error",
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
				errMsg: firstLine(msg),
			}, true
		}
		plan.planReplacementAuthorized = plan.planTransition && dec.AuthorizePlanReplacement
	}
	// Trusted MCP fast path: installed tools and authorized lifecycle connects
	// (mcp_connect__*) skip ordinary Ask/Auto/dontAsk gates. Only explicit deny
	// and live authorization apply — first connect of an installed server must
	// not re-prompt under headless or partial-auto policies.
	gateArgs := tool.PermissionArgs(ctx, plan.execTool, plan.permArgs)
	if isInstalledMCPTool(plan.execTool) || isMCPLifecycleConnectTarget(plan.execTool) {
		if !tool.IsMCPServerAuthorized(plan.execTool) {
			return toolOutcome{
				output:  "blocked: this project MCP server identity has not been authorized; approve the server from a parent session and retry",
				blocked: true,
				errMsg:  "blocked: MCP server identity is not authorized",
			}, true
		}
		if outcome, blocked := a.mcpFastPathRuling(ctx, plan, gateArgs); blocked {
			return outcome, true
		}
	} else if a.svc.gate != nil {
		v, err := permission.VerdictOf(a.originCtx(ctx, plan), a.svc.gate, plan.permName, gateArgs, plan.readOnly)
		if err != nil {
			return toolOutcome{
				output:    fmt.Sprintf("blocked: %s (%v)", v.Reason, err),
				blocked:   true,
				errMsg:    fmt.Sprintf("blocked: %v", err),
				execution: shellRefusal(plan.execTool, plan.execArgs, tool.ShellPhaseAuthorization),
			}, true
		}
		// permission.decision: the host verdict is computed first; the
		// extension ruling may override it in either direction (an allow
		// overriding a host deny is the full-trust contract and is audited).
		if blocked, early := a.interceptExtensionPermission(ctx, plan, &v.Allow); early {
			return blocked, true
		}
		if !v.Allow {
			return toolOutcome{
				output:      "blocked: " + v.Reason,
				blocked:     true,
				errMsg:      "blocked by permission policy",
				refusalCode: v.Code,
				execution:   shellRefusal(plan.execTool, plan.execArgs, tool.ShellPhaseAuthorization),
			}, true
		}
	}
	return toolOutcome{}, false
}

// prepareToolExecution acquires write leases, parent write claims, runs
// PreToolUse hooks and preview checkpoints, and injects call context. All of
// this happens after permission and before the concrete Execute call.
func (a *Agent) prepareToolExecution(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if outcome, blocked := a.assertPlanPhaseAdmitted(plan); blocked {
		return outcome, true
	}
	// Resolve the concrete execution target before hooks. A proxy may carry a
	// different target/name/argument set than the provider-visible call.
	plan.runTool, plan.runArgs = plan.executionTarget()
	if outcome, missing := rootMissingOutcome(plan.runTool, plan.runArgs); missing {
		return outcome, true
	}
	// Hooks can write beyond a tool's paths, so permission precedes lease
	// acquisition and hooks follow it under a conservative workspace claim.
	ctx, blocked, refused := a.holdWorkspaceLease(ctx, plan)
	if refused {
		return blocked, true
	}
	// Hold the parent claim before PreToolUse: hooks are user shell code and may
	// mutate the same workspace. The reservation remains live through hooks,
	// checkpointing, and the concrete Execute call, closing both hook-side and
	// check-before-write TOCTOU windows. Dynamic Economy/MCP tools are covered
	// here after registry lookup without schema-changing wrappers.
	// executeOne defers plan.releaseParentWrite so every return path releases.
	if releaseParentWrite, perr := a.reserveToolWrite(ctx, plan.runTool, plan.runArgs, plan.readOnly, a.declareWritePaths(ctx, plan)); perr != nil {
		out := toolOutcome{
			output:  "blocked: " + perr.Error(),
			blocked: true,
			errMsg:  "blocked: write path claimed by background subagent",
		}
		if errors.Is(perr, writeclaim.ErrWritePathBusy) {
			out.refusalCode = writeclaim.CodeWritePathBusy
		}
		return out, true
	} else if releaseParentWrite != nil {
		plan.releaseParentWrite = releaseParentWrite
	}
	// Acquire the checkpoint barrier before preimage capture and any hook. It is
	// held through post hooks and AfterMutation so rewind cannot interleave with
	// writer-side user code.
	if (!plan.readOnly || toolHooksMayMutateWorkspace(a.svc.hooks)) && a.svc.mutationObserver != nil {
		release, err := a.svc.mutationObserver.BeginWriteContext(ctx)
		if err != nil {
			return toolOutcome{output: "blocked: " + err.Error(), blocked: true, errMsg: "blocked: checkpoint write preparation failed"}, true
		}
		plan.releaseMutationWrite = release
	}
	if blocked, early := a.preToolUse(ctx, plan); early {
		return blocked, true
	}
	if !plan.readOnly {
		a.observeBeforeMutation(ctx, plan)
		plan.mutationObserved = plan.mutationPath != "" || len(plan.declaredPaths) > 0
	}
	plan.cctx = a.toolCallContext(ctx, plan)
	if plan.mutates {
		// Before the write, not after: a criterion's old bytes exist nowhere else
		// once this runs, and no later evidence can reconstruct what it said.
		if err := a.captureCriteriaBefore(ctx, plan); err != nil {
			return toolOutcome{
				output:  "blocked: the host could not keep what this call may overwrite — " + err.Error(),
				blocked: true,
				errMsg:  "blocked: baseline criteria could not be held",
			}, true
		}
		plan.pathsBefore = snapshotPaths(a.task.ledger, a.writeWorkspaceRoot, append(evidence.ToolCallPaths(plan.evidenceArgs), plan.declaredPaths...))
		plan.scanBefore = a.scanBeforeUnprovenCall(ctx, plan)
	}
	return toolOutcome{}, false
}

type toolMutationHookReporter interface {
	ToolMutationHooksEnabled() bool
}

func toolHooksMayMutateWorkspace(hooks ToolHooks) bool {
	if hooks == nil {
		return false
	}
	if reporter, ok := hooks.(toolMutationHookReporter); ok {
		return reporter.ToolMutationHooksEnabled()
	}
	// Custom ToolHooks implementations predate the capability report. Preserve
	// conservative coverage for them because their callbacks may write files.
	return true
}

// finishToolExecution performs the concrete Execute, records evidence, runs
// post hooks and recovery observation, and truncates the model-facing result.
func (a *Agent) finishToolExecution(ctx context.Context, plan *toolCallPlan) toolOutcome {
	plan.leasePreparation.Retain()
	plan.executed = true
	a.announceDelegation(plan)
	cctx := plan.cctx
	runTool := plan.runTool
	if _, ok := runTool.(tool.FilePresenter); ok {
		plan.resolvedMeta = &tool.ResolvedCall{TargetName: runTool.Name(), CapabilityID: "tool:present_files", ReadOnly: true, Target: runTool, Args: plan.runArgs}
	}
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
	var media toolMedia
	var execution *tool.ShellExecution
	var err error
	// A call that was authorized under reader classification carries that
	// basis into dispatch: the MCP execution layer re-verifies it linearizably
	// against server authorization and live safety metadata, and refuses to
	// promote it into a writer lane if reclassification landed after the gate.
	if readOnly && isInstalledMCPTool(runTool) && tool.IsMCPServerAuthorized(runTool) && !tool.HasMCPDestructiveHint(runTool) {
		cctx = tool.WithReaderExecutionIntent(cctx)
	}
	// Planner-trusted MCP: authorized + non-destructive, even without
	// readOnlyHint. Final dispatch re-checks live authorization/destructiveHint.
	if a.role.plannerMCPExecution && isMCPExecutionTarget(runTool, permName) && tool.IsMCPServerAuthorized(runTool) && !tool.HasMCPDestructiveHint(runTool) {
		cctx = tool.WithNonDestructiveMCPExecutionIntent(cctx)
	}
	if a.svc.hooks != nil && call.Name == "task" && !isBackgroundTaskCall(call.ExecutionArguments()) {
		a.svc.hooks.SubagentStart(ctx, call.ID, json.RawMessage(call.ExecutionArguments()))
		// Stop answers every exit after Start, including a cancelled turn, whose
		// ctx would otherwise kill the hook before it runs.
		defer func() { a.svc.hooks.SubagentStop(context.WithoutCancel(ctx), call.ID, result, err) }()
	}
	result, media, execution, err = a.executeStoppable(ctx, cctx, plan)
	// tool.after: extensions rule on the executed result (success or error)
	// before evidence, hooks, and recovery observation, so every downstream
	// consumer sees the final (possibly replaced) outcome.
	plan.ranClean = err == nil
	result, err = a.interceptToolAfter(ctx, call, result, err)
	// A tool that refused its own call never ran: report it like the permission
	// and plan-mode blocks above rather than as an execution failure.
	if msg, refused := tool.BlockedMessage(err); refused {
		return a.blockedToolOutcome(plan, msg)
	}
	err = usecap.WithContractHint(err, runTool, runArgs)
	origin := plan.provenanceOf(runTool, runArgs)
	tool.NoteProvenance(ctx, origin)
	owedBefore := a.obligations()
	a.recordToolReceipts(ctx, plan, result, execution, err)
	result = a.withObligationNotice(result, owedBefore, execution, readOnly)
	// Track skill/capability outcomes for Delivery gates.
	a.noteCapabilityInvocation(plan.canonicalName, json.RawMessage(call.ExecutionArguments()), runTool, err)
	a.notifyToolHooks(ctx, permName, permArgs, result, err)
	// Always re-read after post hooks — partial writes and hook side effects can
	// change the previewed path even when the concrete tool returned an error.
	checkpointErr := a.observeAfterMutation(plan)
	plan.mutationAfterDone = true
	if a.svc.recoveryGate != nil {
		a.observeRecoveryResult(ctx, evidenceName, evidenceArgs, readOnly, mutates, result, err, false, false, recoveryGen)
	}
	if err != nil {
		detail := silentExitDetail(evidenceName, evidenceArgs, result)
		// Malformed-args failures are a transient model JSON glitch (e.g. options
		// written as ["a":"b"] → "invalid character ':' after array element"). The
		// args can't be safely re-parsed, but echoing the tool's schema makes the
		// retry land valid instead of repeating the same broken shape.
		if !json.Valid([]byte(call.Arguments)) {
			detail = strings.TrimRight(detail, "\n") + "\n" + malformedArgumentsDetail(call.Arguments) + "\n" + string(t.Schema())
		}
		rawErr := fmt.Sprintf("error: %v\n%s", err, detail)
		body, bound, truncMsg := a.boundToolOutput(rawErr, call.Name, call.ID, call.ExecutionArguments(), true)
		// A failed call's screenshot is often the only record of why it failed.
		out := toolOutcome{
			output: body, images: media.images, videos: media.videos, errMsg: firstLine(err.Error()), bound: bound, truncMsg: truncMsg,
			execution: execution, provenance: origin, refusalCode: refusalCodeOf(err), checkpointErr: checkpointErr,
		}
		out.keepFull(rawErr)
		return out
	}
	a.observeTouchedPaths(runTool, runArgs)
	result = silentSuccessDetail(evidenceName, evidenceArgs, result)
	body, bound, truncMsg := a.boundToolOutput(result, call.Name, call.ID, call.ExecutionArguments(), false)
	out := toolOutcome{
		output: body, images: media.images, videos: media.videos, bound: bound, truncMsg: truncMsg,
		execution: execution, provenance: origin, checkpointErr: checkpointErr,
	}
	out.keepFull(result)
	return out
}

// executeStoppable runs the call under a context CancelToolCall can end alone.
func (a *Agent) executeStoppable(turn, cctx context.Context, plan *toolCallPlan) (result string, media toolMedia, execution *tool.ShellExecution, err error) {
	runTool, runArgs := plan.runTool, plan.runArgs
	cctx, endCall := a.stops.begin(cctx, plan.call.ID)
	defer endCall()
	if de, ok := runTool.(tool.DetailedExecutor); ok {
		var detailed tool.DetailedResult
		detailed, err = de.ExecuteDetailed(cctx, runArgs)
		result, media.images, execution = detailed.Output, detailed.Images, detailed.Execution
		// Annotate verification outcome when the host classified this call as a
		// verifier. The exit status answers for the whole command, so it is a
		// verdict about the verifier only when no later stage can decide it.
		if execution != nil && plan.verification {
			execution.Verification = shellVerificationVerdict(bashCommandFromArgs(plan.evidenceArgs),
				bashDeclaredCheck(plan.evidenceArgs) != "", execution.PipeStatus, err)
			result = shellVerificationNotice(result, execution)
		} else if execution != nil && execution.Verification == "" {
			execution.Verification = tool.ShellVerificationNotVerification
		}
		annotateShellSubject(execution, runArgs)
		// Sole opaque inline interpreters are allowed outside Delivery but cannot
		// prove mutation completeness.
		if execution != nil && evidence.BashCommandMayBeOpaqueMutation(runArgs) &&
			execution.MutationRisk == tool.ShellMutationMayHaveCompleted {
			execution.MutationRisk = tool.ShellMutationUnknown
		}
	} else if run, ok := mediaTool(runTool); ok {
		result, media, err = run(cctx, runArgs)
	} else {
		result, err = runTool.Execute(cctx, runArgs)
	}
	return result, media, execution, stoppedByUser(turn, cctx, err)
}
