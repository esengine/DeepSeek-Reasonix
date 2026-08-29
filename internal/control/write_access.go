package control

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	if strings.EqualFold(req.Tool, "bash") && len(req.Declaration.Directories) == 0 {
		return agent.WriteAccessDecision{Allow: true}, nil
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
	if c.writeAccess.roots == nil {
		if len(abs) == 0 {
			return agent.WriteAccessDecision{Allow: true}, nil
		}
		return agent.WriteAccessDecision{Allow: false, Reason: agentHeadlessWriteHint(display)}, nil
	}
	missing := c.writeAccess.roots.Missing(abs)
	if len(missing) == 0 {
		return agent.WriteAccessDecision{Allow: true}, nil
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
	}, nil
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
	case ToolApprovalAuto, ToolApprovalYolo:
		policy.Mode = permission.Allow
	case ToolApprovalDontAsk:
		policy.Mode = permission.Deny
	}
	dec := policy.Decide(toolName, readOnly, args)
	if dec != permission.Ask {
		return dec
	}
	subject := permission.Subject(args)
	requireHuman := strings.EqualFold(toolName, "bash") && permission.BashSubjectRequiresExplicitApproval(subject)
	if c.approval.preApprovedForDecisionOptions(toolName, subject, args, false, requireHuman) {
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
		PersistAllowed:           c.writeAccess.persist != nil,
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
		c.approval.cancel(id)
		return approvalReply{}, waitCtx.Err()
	}
}

// ResolveApproval answers a pending approval with an explicit scope.
func (c *Controller) ResolveApproval(id string, allow bool, scope sandbox.ApprovalScope) error {
	if c == nil {
		return fmt.Errorf("controller is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("empty approval id")
	}
	c.mu.Lock()
	gate := c.recoveryGate
	c.mu.Unlock()
	if gate != nil && gate.HasApproval(id) {
		action := agent.RecoveryActionRevise
		if allow {
			action = agent.RecoveryActionContinue
		}
		return c.ResolveRecovery(id, action, "")
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
		return c.resolveWriteAccess(pending, allow, scope)
	}
	session := allow && (scope == sandbox.ApprovalScopeSession || scope == sandbox.ApprovalScopeProject)
	persist := allow && scope == sandbox.ApprovalScopeProject
	return c.approveChecked(id, allow, session, persist)
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
	if scope == sandbox.ApprovalScopeProject {
		if err := c.persistWriteAccess(pending.tool, pending.subject, verifiedDirs, merge); err != nil {
			c.recordDecisionReceipt(pending, "deny")
			pending.reply <- approvalReply{persistErr: err}
			c.sink.Emit(event.Event{
				Kind:  event.Notice,
				Level: event.LevelWarn,
				Text:  fmt.Sprintf("could not save write access to the project reasonix.toml: %v", err),
			})
			return err
		}
	}
	outcome := "allow_once"
	reply := approvalReply{allow: true, onceDirs: verifiedDirs}
	if scope == sandbox.ApprovalScopeSession || scope == sandbox.ApprovalScopeProject {
		if c.writeAccess.roots != nil {
			if scope == sandbox.ApprovalScopeProject {
				c.writeAccess.roots.GrantVerifiedBaseline(verifiedDirs)
			} else {
				c.writeAccess.roots.GrantVerifiedSession(verifiedDirs)
			}
		}
		if merge {
			c.approval.grantSession(pending.tool, pending.subject)
		}
		reply.session = true
		reply.onceDirs = nil
		if scope == sandbox.ApprovalScopeProject {
			reply.persist = true
			outcome = "allow_project"
		} else {
			outcome = "allow_session"
		}
	}
	c.recordDecisionReceipt(pending, outcome)
	pending.reply <- reply
	return nil
}

func (c *Controller) persistWriteAccess(toolName, subject string, dirs []string, mergePerm bool) error {
	if c.writeAccess.persist == nil {
		return fmt.Errorf("project persistence is not available")
	}
	rule := ""
	if mergePerm {
		rule = permission.RememberRuleForScope(toolName, subject)
	}
	return c.writeAccess.persist(dirs, rule)
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

// QueryAuthorizedWriteDirs returns the write directories currently authorized
// for this controller, split by scope:
//
//	project — configured [sandbox].allow_write (persisted in the project config)
//	         as [project] allow_write entries;
//	global — user-global [sandbox].allow_global common dirs honored for every
//	         project/session without approval (including subdirectories);
//	session — session-level roots granted for the rest of this logical session
//	         (process memory).
//
// This backs the "authorized write directories" management surface (see
// #9167). Per-call one-shot grants are intentionally not listed.
func (c *Controller) QueryAuthorizedWriteDirs() (project, global, session []string) {
	if c != nil && c.workspaceRoot != "" {
		if cfg, err := config.LoadForRootReadOnly(c.workspaceRoot); err == nil && cfg != nil {
			project = cfg.AllowWriteRoots()
			global = cfg.GlobalAllowRoots()
		}
	}
	if c != nil && c.writeAccess.roots != nil {
		session = c.writeAccess.roots.SessionRoots()
	}
	return project, global, session
}

// AddAuthorizedWriteDir adds a writable directory at the given scope.
//   - ApprovalScopeProject: persists into the project config [sandbox].allow_write
//     (via SetProjectWriteAccess) and grants it as a verified baseline root.
//   - ApprovalScopeSession: grants a verified session root for this logical session.
//
// Other scopes are rejected. Directory creation is best-effort; a not-yet
// existing approved path is created like the interactive approval flow does.
func (c *Controller) AddAuthorizedWriteDir(scope sandbox.ApprovalScope, dir string) error {
	if c == nil {
		return fmt.Errorf("no active controller")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("directory is required")
	}
	stateRoot := config.MemoryUserDir()
	verified, err := sandbox.EnsureWriteDir(dir, stateRoot)
	if err != nil {
		return fmt.Errorf("add authorized write dir: %w", err)
	}
	switch scope {
	case sandbox.ApprovalScopeProject:
		if c.writeAccess.persist == nil {
			return fmt.Errorf("project persistence is not available")
		}
		// Rewrite the full allow_write list with the new entry appended
		// (replacement write on current list) so removal can also be served.
		project, _, _ := c.QueryAuthorizedWriteDirs()
		if !containsWriteRoot(project, verified) {
			project = append(project, verified)
		}
		if err := c.persistSetWriteAccess(project); err != nil {
			return err
		}
		if c.writeAccess.roots != nil {
			c.writeAccess.roots.GrantVerifiedBaseline([]string{verified})
		}
		return nil
	case sandbox.ApprovalScopeSession:
		if c.writeAccess.roots != nil {
			c.writeAccess.roots.GrantVerifiedSession([]string{verified})
		}
		return nil
	default:
		return fmt.Errorf("unsupported scope %v for adding a write directory", scope)
	}
}

// RemoveAuthorizedWriteDir removes a writable directory at the given scope.
//   - ApprovalScopeProject: removes it from the project config [sandbox].allow_write
//     and from the in-memory baseline.
//   - ApprovalScopeSession: removes it from the session roots.
func (c *Controller) RemoveAuthorizedWriteDir(scope sandbox.ApprovalScope, dir string) error {
	if c == nil {
		return fmt.Errorf("no active controller")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("directory is required")
	}
	switch scope {
	case sandbox.ApprovalScopeProject:
		project, _, _ := c.QueryAuthorizedWriteDirs()
		kept := project[:0]
		for _, p := range project {
			if pathEqualDir(p, dir) {
				continue
			}
			kept = append(kept, p)
		}
		if err := c.persistSetWriteAccess(kept); err != nil {
			return err
		}
		// The persisted [sandbox].allow_write rewrite is authoritative for the
		// project scope; the in-memory baseline will refresh from config on the
		// next LoadForRoot. Nothing to mutate in session roots here.
		return nil
	case sandbox.ApprovalScopeSession:
		if c.writeAccess.roots != nil {
			c.writeAccess.roots.RemoveSessionRoot(dir)
		}
		return nil
	default:
		return fmt.Errorf("unsupported scope %v for removing a write directory", scope)
	}
}

// GlobalWriteDirs returns the user-global common directories (config
// [sandbox] allow_global) honored for every project/session without approval.
func (c *Controller) GlobalWriteDirs() []string {
	if c == nil {
		return nil
	}
	_, global, _ := c.QueryAuthorizedWriteDirs()
	return global
}

// AddGlobalWriteDir adds a directory to the user-global common-directory list
// (config [sandbox] allow_global). It is honored across all projects/sessions
// without approval, including subdirectories. Directory creation is best-effort
// like the interactive approval flow.
func (c *Controller) AddGlobalWriteDir(dir string) error {
	if c == nil {
		return fmt.Errorf("no active controller")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("directory is required")
	}
	stateRoot := config.MemoryUserDir()
	verified, err := sandbox.EnsureWriteDir(dir, stateRoot)
	if err != nil {
		return fmt.Errorf("add global write dir: %w", err)
	}
	global := c.GlobalWriteDirs()
	if !containsWriteRoot(global, verified) {
		global = append(global, verified)
	}
	return config.SetGlobalWriteAccess(global)
}

// RemoveGlobalWriteDir removes a directory from the user-global common-directory
// list (config [sandbox] allow_global).
func (c *Controller) RemoveGlobalWriteDir(dir string) error {
	if c == nil {
		return fmt.Errorf("no active controller")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("directory is required")
	}
	global := c.GlobalWriteDirs()
	kept := global[:0]
	for _, g := range global {
		if pathEqualDir(g, dir) {
			continue
		}
		kept = append(kept, g)
	}
	return config.SetGlobalWriteAccess(kept)
}

func containsWriteRoot(roots []string, target string) bool {
	for _, r := range roots {
		if pathEqualDir(r, target) {
			return true
		}
	}
	return false
}

func pathEqualDir(a, b string) bool {
	absA, errA := filepath.Abs(filepath.Clean(a))
	absB, errB := filepath.Abs(filepath.Clean(b))
	if errA == nil && errB == nil {
		return absA == absB
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// projectConfigPathForWriteAccess returns the project config path that holds
// [sandbox].allow_write for a workspace (workspaceRoot/reasonix.toml), matching
// boot's rememberPermissionConfigPath semantics. Empty workspaceRoot falls back
// to the user config path.
func projectConfigPathForWriteAccess(workspaceRoot string) string {
	root := strings.TrimSpace(workspaceRoot)
	if root != "" {
		return filepath.Join(root, "reasonix.toml")
	}
	path := config.SourcePath()
	if path == "" {
		path = "reasonix.toml"
	}
	return path
}

// persistSetWriteAccess rewrites the project config [sandbox].allow_write to
// exactly the given list (replacement write, supports removal). It is the
// management-surface counterpart to the interactive approval's append-only
// PersistProjectWriteAccess (see #9167).
func (c *Controller) persistSetWriteAccess(allowWrite []string) error {
	return config.SetProjectWriteAccess(projectConfigPathForWriteAccess(c.workspaceRoot), allowWrite, "")
}
