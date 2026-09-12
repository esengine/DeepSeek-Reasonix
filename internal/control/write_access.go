package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/sandbox"
)

const writeAccessKind = event.ApprovalKindWriteAccess

// PersistWriteAccessFunc writes permission + sandbox.allow_write in one
// project-config transaction. A non-nil error must not grant or execute.
type PersistWriteAccessFunc func(dirs []string, permRule string) error

type controllerWriteAccess struct {
	persist             PersistWriteAccessFunc
	roots               *sandbox.WritableRootSet
	interactive         bool
	bashSandboxEnforced bool
}

func newControllerWriteAccess(opts Options) controllerWriteAccess {
	return controllerWriteAccess{
		persist: opts.OnPersistWriteAccess, roots: opts.WriteRoots,
		bashSandboxEnforced: opts.BashSandboxEnforced,
	}
}

func (c *Controller) CheckWriteAccess(ctx context.Context, req agent.WriteAccessCheck) (agent.WriteAccessDecision, error) {
	requestedPreset := strings.TrimSpace(req.Declaration.RequestedPreset)
	if requestedPreset == "danger-full-access" && c.approval.mode() != ToolApprovalDangerFullAccess {
		return c.checkDangerFullAccessRetry(ctx, req)
	}
	if strings.EqualFold(req.Tool, "bash") && len(req.Declaration.Directories) == 0 {
		return agent.WriteAccessDecision{Allow: true, PermissionPreset: requestedPreset}, nil
	}
	if strings.EqualFold(req.Tool, "bash") && !c.bashEnforcesSandbox() {
		return agent.WriteAccessDecision{Allow: true}, nil
	}
	workDir := strings.TrimSpace(c.workspaceRoot)
	home, _ := os.UserHomeDir()
	stateRoot := config.MemoryUserDir()
	abs, display, broadHome, err := sandbox.NormalizeWriteDirs(req.Declaration.Directories, workDir, home, stateRoot)
	if err != nil {
		return agent.WriteAccessDecision{Allow: false, Reason: err.Error()}, nil
	}
	if c.approval.mode() == ToolApprovalDangerFullAccess {
		if c.ordinaryWriteDecision(req.Tool, req.Args, req.ReadOnly) == permission.Deny {
			return agent.WriteAccessDecision{Allow: false, Reason: "denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain."}, nil
		}
		return agent.WriteAccessDecision{Allow: true, PerCallRoots: abs, SkipOrdinaryGate: true, PermissionPreset: requestedPreset}, nil
	}
	if c.writeAccess.roots == nil {
		if len(abs) == 0 {
			return agent.WriteAccessDecision{Allow: true}, nil
		}
		return agent.WriteAccessDecision{Allow: false, Reason: agentHeadlessWriteHint(display)}, nil
	}
	missing := c.writeAccess.roots.Missing(abs)
	if len(missing) == 0 {
		return agent.WriteAccessDecision{Allow: true, PermissionPreset: requestedPreset}, nil
	}
	missingDisplay := displayForAbs(abs, display, missing)
	decision := c.ordinaryWriteDecision(req.Tool, req.Args, req.ReadOnly)
	if decision == permission.Deny {
		return agent.WriteAccessDecision{Allow: false, Reason: "denied by permission policy — this tool/command is on the deny list. Do not retry it; choose another approach or stop and explain."}, nil
	}
	if !req.Expandable {
		return agent.WriteAccessDecision{Allow: false, Reason: agent.SubagentWriteAccessMessage(missingDisplay)}, nil
	}
	if !c.writeAccess.interactive {
		return agent.WriteAccessDecision{Allow: false, Reason: agentHeadlessWriteHint(missingDisplay)}, nil
	}
	if c.approval.mode() == ToolApprovalDontAsk {
		return agent.WriteAccessDecision{Allow: false, Reason: agentHeadlessWriteHint(missingDisplay)}, nil
	}
	mergeAsk := decision == permission.Ask
	grant, err := c.requestWriteAccess(ctx, req, missing, missingDisplay, strings.TrimSpace(req.Declaration.Justification), broadHome, mergeAsk)
	if err != nil {
		return agent.WriteAccessDecision{}, err
	}
	if !grant.Allow {
		reason := strings.TrimSpace(grant.Reason)
		if reason == "" {
			reason = "the user declined to extend write access — do not retry it; ask how they would like to proceed or choose another approach."
		}
		return agent.WriteAccessDecision{Allow: false, Reason: reason}, nil
	}
	return agent.WriteAccessDecision{
		Allow:            true,
		PerCallRoots:     grant.PerCall,
		SkipOrdinaryGate: mergeAsk || decision == permission.Allow,
		PermissionPreset: requestedPreset,
	}, nil
}

func (c *Controller) checkDangerFullAccessRetry(ctx context.Context, req agent.WriteAccessCheck) (agent.WriteAccessDecision, error) {
	command := bashCommandForPermissionRetry(req.Args)
	subject := command
	if subject == "" {
		subject = strings.TrimSpace(req.Subject)
	}
	if c.ordinaryWriteDecision(req.Tool, req.Args, req.ReadOnly) == permission.Deny {
		return agent.WriteAccessDecision{Allow: false, Reason: "denied by permission policy — this tool/command is on the deny list. Do not retry it."}, nil
	}
	// A full-access retry is never covered by the workspace preset itself. Only
	// an exact session authorization for this command (or an already active
	// full-access preset, handled by the caller) may skip the prompt.
	if c.approval.preApprovedForExactSession(req.Tool, subject) {
		return agent.WriteAccessDecision{Allow: true, SkipOrdinaryGate: true, PermissionPreset: "danger-full-access"}, nil
	}
	if !sandbox.ConsumeDenial(req.Declaration.DenialID, command) {
		return agent.WriteAccessDecision{Allow: false, Reason: "danger-full-access retry requires a current host-issued denial_id for this exact command"}, nil
	}
	if !req.Expandable || !c.writeAccess.interactive || c.approval.mode() == ToolApprovalDontAsk {
		return agent.WriteAccessDecision{Allow: false, Reason: "danger-full-access retry requires an interactive explicit authorization"}, nil
	}
	req.Subject = subject
	grant, err := c.requestWriteAccess(ctx, req, nil, nil, strings.TrimSpace(req.Declaration.Justification), false, true)
	if err != nil {
		return agent.WriteAccessDecision{}, err
	}
	if !grant.Allow {
		return agent.WriteAccessDecision{Allow: false, Reason: "the user declined the full-access retry"}, nil
	}
	return agent.WriteAccessDecision{Allow: true, SkipOrdinaryGate: true, PermissionPreset: "danger-full-access"}, nil
}

func bashCommandForPermissionRetry(args []byte) string {
	var payload struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(args, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.Command)
}

func agentHeadlessWriteHint(display []string) string {
	needed := strings.Join(display, ", ")
	if needed == "" {
		return "this directory is outside the writable roots. Restart with --add-dir /abs/path, add it to [sandbox].allow_write in reasonix.toml, or use an interactive session to approve the directory."
	}
	return "this directory is outside the writable roots (" + needed + "). Restart with --add-dir " + needed + ", add it to [sandbox].allow_write in reasonix.toml, or use an interactive session to approve the directory."
}

func displayForAbs(abs, display, missing []string) []string {
	index := map[string]string{}
	for i, dir := range abs {
		if i < len(display) {
			index[dir] = display[i]
		}
	}
	out := make([]string, 0, len(missing))
	for _, dir := range missing {
		if shown := index[dir]; shown != "" {
			out = append(out, shown)
			continue
		}
		out = append(out, dir)
	}
	return out
}

func (c *Controller) ordinaryWriteDecision(toolName string, args []byte, readOnly bool) permission.Decision {
	policy := c.policy
	mode := c.approval.mode()
	switch mode {
	case ToolApprovalWorkspaceWrite, ToolApprovalDangerFullAccess:
		policy.Mode = permission.Allow
	case ToolApprovalDontAsk:
		policy.Mode = permission.Deny
	}
	dec := policy.Decide(toolName, readOnly, args)
	if dec != permission.Ask {
		return dec
	}
	subject := permission.Subject(args)
	if c.approval.preApprovedForDecisionOptions(toolName, subject, args, false, false) {
		return permission.Allow
	}
	return permission.Ask
}

func (c *Controller) bashEnforcesSandbox() bool {
	return c != nil && c.writeAccess.bashSandboxEnforced && sandbox.Available()
}

type writeAccessReply struct {
	Allow   bool
	Reason  string
	PerCall []string
}

func (c *Controller) requestWriteAccess(ctx context.Context, req agent.WriteAccessCheck, dirs, display []string, justification string, broadHome, mergeAsk bool) (writeAccessReply, error) {
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = strings.Join(display, ", ")
	}
	reason := justification
	if mergeAsk {
		if reason != "" {
			reason += "\n"
		}
		reason += "This choice also authorizes the current matching tool operation."
	}
	payload := event.NormalizeWriteAccessApproval(&event.WriteAccessApproval{
		Directories:              append([]string{}, dirs...),
		DisplayDirectories:       append([]string{}, display...),
		Justification:            justification,
		BroadHomeAccess:          broadHome,
		OrdinaryPermissionNeeded: mergeAsk,
		PersistAllowed:           false,
	})
	reply, err := c.requestWriteAccessDecision(ctx, req.Tool, subject, req.Args, reason, payload)
	if err != nil {
		return writeAccessReply{}, err
	}
	if reply.persistErr != nil {
		return writeAccessReply{Reason: reply.persistErr.Error()}, nil
	}
	if !reply.allow {
		return writeAccessReply{}, nil
	}
	return writeAccessReply{Allow: true, PerCall: append([]string(nil), reply.onceDirs...)}, nil
}

func (c *Controller) requestWriteAccessDecision(ctx context.Context, toolName, subject string, args []byte, reason string, payload *event.WriteAccessApproval) (approvalReply, error) {
	c.approval.promptMu.Lock()
	defer c.approval.promptMu.Unlock()

	c.approval.promptEmitMu.Lock()
	id, reply := c.approval.registerWriteAccess(toolName, subject, reason, args, payload)
	c.registerOwnedPrompt(id, PromptApproval)
	approval := event.Approval{
		ID:          id,
		Tool:        toolName,
		Subject:     subject,
		Reason:      reason,
		RawInput:    append([]byte(nil), args...),
		Fresh:       true,
		Kind:        writeAccessKind,
		WriteAccess: payload,
	}
	c.sink.Emit(c.approvalRequestEvent(approval))
	c.approval.promptEmitMu.Unlock()
	go c.hooks.Notification(ctx, approvalNotificationText(toolName, subject), "permission_prompt")

	waitCtx, cancelWait := c.approval.waitContext(ctx)
	defer cancelWait()
	select {
	case r := <-reply:
		return r, nil
	case <-waitCtx.Done():
		c.cancelOwnedPrompt(id)
		return approvalReply{}, waitCtx.Err()
	}
}

// ResolveApproval answers a pending approval with an explicit scope.
func (c *Controller) ResolveApproval(id string, allow bool, scope sandbox.ApprovalScope) error {
	defer c.refreshRuntimeState(event.Event{})
	c.promptResolveMu.Lock()
	defer c.promptResolveMu.Unlock()
	return c.resolveApprovalLocked(id, allow, scope)
}

// ResolveApprovalAt resolves an approval only while the permission runtime is
// still the one that emitted it. This prevents a delayed browser or remote
// response from authorizing work after a session restart or preset change.
func (c *Controller) ResolveApprovalAt(id string, allow bool, scope sandbox.ApprovalScope, generation, permissionRevision uint64) error {
	defer c.refreshRuntimeState(event.Event{})
	if c == nil {
		return ErrPromptNotPending
	}
	c.promptResolveMu.Lock()
	defer c.promptResolveMu.Unlock()
	if generation != 0 && generation != c.runtimeGeneration {
		return ErrPromptStaleRuntime
	}
	if permissionRevision != 0 && permissionRevision != c.permissionRevision.Load() {
		return ErrPromptStaleRuntime
	}
	return c.resolveApprovalLocked(id, allow, scope)
}

func (c *Controller) resolveApprovalLocked(id string, allow bool, scope sandbox.ApprovalScope) error {
	if c == nil {
		return fmt.Errorf("controller is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("empty approval id")
	}
	if allow && scope == sandbox.ApprovalScopeProject {
		return fmt.Errorf("permanent approval is no longer supported; allow once or for this session")
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil && gate.HasApproval(id) {
		action := agent.RecoveryActionRevise
		if allow {
			action = agent.RecoveryActionContinue
		}
		return c.resolveRecoveryLocked(id, action, "")
	}
	pending := c.approval.peek(id)
	if pending.reply == nil {
		return nil
	}
	if pending.kind == writeAccessKind {
		var ok bool
		var err error
		pending, ok, err = c.approval.resolveAfter(id, func(p pendingApproval) error {
			return c.emitTurnEventChecked(event.Event{Kind: event.PromptAnswered, ItemID: id, Status: event.TurnInProgress})
		})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("approval %q is no longer pending", id)
		}
		c.promptOwner.Remove(id)
		return c.resolveWriteAccess(pending, allow, scope)
	}
	session := allow && scope == sandbox.ApprovalScopeSession
	return c.approveChecked(id, allow, session, false)
}

func (c *Controller) resolveWriteAccess(pending pendingApproval, allow bool, scope sandbox.ApprovalScope) error {
	if pending.reply == nil {
		return fmt.Errorf("write access approval is no longer pending")
	}
	if !allow {
		c.recordDecisionReceipt(pending, "deny")
		pending.reply <- approvalReply{}
		return nil
	}
	dirs := []string{}
	merge := false
	if pending.writeAccess != nil {
		dirs = append([]string{}, pending.writeAccess.Directories...)
		merge = pending.writeAccess.OrdinaryPermissionNeeded
	}
	stateRoot := config.MemoryUserDir()
	verifiedDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		verified, err := sandbox.EnsureWriteDir(dir, stateRoot)
		if err != nil {
			c.recordDecisionReceipt(pending, "deny")
			pending.reply <- approvalReply{persistErr: err}
			c.sink.Emit(event.Event{
				Kind:  event.Notice,
				Level: event.LevelWarn,
				Text:  fmt.Sprintf("could not create approved write directory %s: %v", dir, err),
			})
			return err
		}
		verifiedDirs = append(verifiedDirs, verified)
	}
	outcome := "allow_once"
	reply := approvalReply{allow: true, onceDirs: verifiedDirs}
	if scope == sandbox.ApprovalScopeSession {
		if c.writeAccess.roots != nil {
			c.writeAccess.roots.GrantVerifiedSession(verifiedDirs)
		}
		if merge {
			if approvalRequestsFullAccess(pending.rawInput) {
				c.approval.grantExactSession(pending.tool, pending.subject)
			} else {
				c.approval.grantSession(pending.tool, pending.subject)
			}
		}
		reply.session = true
		reply.onceDirs = nil
		outcome = "allow_session"
	}
	c.recordDecisionReceipt(pending, outcome)
	pending.reply <- reply
	return nil
}

func approvalRequestsFullAccess(raw json.RawMessage) bool {
	var payload struct {
		SandboxPermissions string `json:"sandbox_permissions"`
	}
	return json.Unmarshal(raw, &payload) == nil && strings.TrimSpace(payload.SandboxPermissions) == "danger-full-access"
}

func (c *Controller) clearSessionWriteAccess() {
	if c.writeAccess.roots != nil {
		c.writeAccess.roots.ClearSession()
	}
}

func scopeFromApprove(allow, session, persist bool) sandbox.ApprovalScope {
	if !allow {
		return sandbox.ApprovalScopeOnce
	}
	if persist {
		return sandbox.ApprovalScopeProject
	}
	if session {
		return sandbox.ApprovalScopeSession
	}
	return sandbox.ApprovalScopeOnce
}
