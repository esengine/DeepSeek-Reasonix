package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/nilutil"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

const (
	headlessWriteAccessHint = "this directory is outside the writable roots. Restart with --add-dir /abs/path, add it to [sandbox].allow_write in reasonix.toml, or use an interactive session to approve the directory."
	subagentWriteAccessHint = "this sub-agent cannot expand write access. Ask the parent agent to request the directories (bash additional_write_dirs, or write the file after the parent is granted that directory)."
)

// SubagentWriteAccessMessage is the structured failure a child agent returns
// when it needs a directory the parent has not granted.
func SubagentWriteAccessMessage(display []string) string {
	if len(display) == 0 {
		return subagentWriteAccessHint
	}
	return subagentWriteAccessHint + " Needed directories: " + strings.Join(display, ", ")
}

// WriteAccessCheck is the host-local write-directory preflight for one tool call.
type WriteAccessCheck struct {
	Tool        string
	Subject     string
	Args        json.RawMessage
	ReadOnly    bool
	Declaration tool.WriteAccessDeclaration
	Expandable  bool
}

// WriteAccessDecision is the result of a write-directory preflight.
type WriteAccessDecision struct {
	Allow            bool
	Reason           string
	PerCallRoots     []string
	SkipOrdinaryGate bool
}

// WriteAccessGate authorizes extra writable directories before a tool runs.
type WriteAccessGate interface {
	CheckWriteAccess(ctx context.Context, req WriteAccessCheck) (WriteAccessDecision, error)
}

func (b foregroundOnlyBash) DeclareWriteAccess(args json.RawMessage) (tool.WriteAccessDeclaration, error) {
	if d, ok := b.inner.(tool.WriteAccessDeclarer); ok {
		return d.DeclareWriteAccess(args)
	}
	return tool.WriteAccessDeclaration{}, nil
}

func (t *TaskTool) buildSubagentRegistry(spec ProfileExecSpec, toolNames []string, childDepth int) (*tool.Registry, *sandbox.WritableRootSet, error) {
	if spec.Grant.ReadOnly {
		reg := ReadOnlySubagentToolRegistryForDepthWithRuntime(t.parentReg, toolNames, childDepth, t.maxDepth(), t.capabilityRuntime)
		if reg.Len() == 0 && !spec.Grant.AllowNoTools {
			return nil, nil, fmt.Errorf("no read-only tools available for this sub-agent")
		}
		return reg, nil, nil
	}
	reg := t.buildSubReg(toolNames, childDepth)
	// Explicit paths are an execution boundary and rebind/drop tools that cannot
	// honor it. A synthesized whole-workspace claim preserves legacy boundaries.
	if !spec.Grant.WritePaths.Empty() && !spec.Grant.WritePaths.WholeWorkspace {
		bound, removed := BindWritePaths(reg, spec.Grant.WritePaths, t.workspaceRoot, t.bashCanEnforceWriteRoots())
		reg = bound
		if len(removed) > 0 && reg.Len() == 0 {
			return nil, nil, fmt.Errorf("no path-bound write tools available after dropping unbound writers: %s", strings.Join(removed, ", "))
		}
	}
	reg, roots := BindChildWriteRoots(reg, t.writeRoots, spec.Grant.WritePaths)
	return reg, roots, nil
}

// SetConfigWriteApprover installs the optional per-write approval path used by
// file tools for Reasonix-managed config outside the workspace roots.
func (a *Agent) SetConfigWriteApprover(g tool.ConfigWriteApprover) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.svc.configWrite = g
}

func (a *Agent) SetWriteAccessGate(g WriteAccessGate) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.svc.writeAccess = g
}

func (a *Agent) SetWriteRoots(set *sandbox.WritableRootSet) {
	a.svc.writeRoots = set
}

func (c *Coordinator) SetWriteAccessGate(g WriteAccessGate) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetWriteAccessGate(g)
	}
	if c.executor != nil {
		c.executor.SetWriteAccessGate(g)
	}
}

func (c *Coordinator) SetWriteRoots(set *sandbox.WritableRootSet) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetWriteRoots(set)
	}
	if c.executor != nil {
		c.executor.SetWriteRoots(set)
	}
}

func (a *Agent) applyWriteAccess(ctx context.Context, plan *toolCallPlan) (toolOutcome, bool) {
	if a == nil || plan == nil || plan.readOnly {
		return toolOutcome{}, false
	}
	decl, ok := plan.execTool.(tool.WriteAccessDeclarer)
	if !ok {
		return toolOutcome{}, false
	}
	declaration, err := decl.DeclareWriteAccess(plan.permArgs)
	if err != nil {
		return toolOutcome{
			output:  fmt.Sprintf("error: %v", err),
			errMsg:  firstLine(err.Error()),
			blocked: true,
		}, true
	}
	if a.svc.writeAccess == nil {
		if a.svc.writeRoots == nil || len(declaration.Directories) == 0 {
			return toolOutcome{}, false
		}
		abs, display, _, nerr := sandbox.NormalizeWriteDirs(declaration.Directories, a.workspaceRoot(), a.homeDir(), a.stateRoot())
		if nerr != nil {
			return writeAccessBlocked(nerr.Error()), true
		}
		if left := a.svc.writeRoots.Missing(abs); len(left) > 0 {
			if !a.svc.writeAccessExpandable {
				return writeAccessBlocked(SubagentWriteAccessMessage(displayList(display))), true
			}
			return writeAccessBlocked(headlessWriteAccessHint + " Needed: " + strings.Join(displayList(display), ", ")), true
		}
		return toolOutcome{}, false
	}
	dec, err := a.svc.writeAccess.CheckWriteAccess(ctx, WriteAccessCheck{
		Tool:        plan.permName,
		Subject:     permissionSubject(plan),
		Args:        plan.permArgs,
		ReadOnly:    plan.readOnly,
		Declaration: declaration,
		Expandable:  a.svc.writeAccessExpandable,
	})
	if err != nil {
		return toolOutcome{
			output:  fmt.Sprintf("blocked: %v", err),
			blocked: true,
			errMsg:  firstLine(err.Error()),
		}, true
	}
	if !dec.Allow {
		msg := strings.TrimSpace(dec.Reason)
		if msg == "" {
			msg = "write access was denied"
		}
		if !strings.HasPrefix(msg, "blocked:") {
			msg = "blocked: " + msg
		}
		return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg)}, true
	}
	plan.perCallWriteRoots = dec.PerCallRoots
	plan.skipOrdinaryGate = dec.SkipOrdinaryGate
	return toolOutcome{}, false
}

func writeAccessBlocked(reason string) toolOutcome {
	msg := strings.TrimSpace(reason)
	if !strings.HasPrefix(msg, "blocked:") {
		msg = "blocked: " + msg
	}
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg)}
}

func permissionSubject(plan *toolCallPlan) string {
	if plan == nil {
		return ""
	}
	if plan.evidenceName == "bash" {
		return strings.TrimSpace(bashCommandFromArgs(plan.permArgs))
	}
	return strings.TrimSpace(string(plan.permArgs))
}

func displayList(dirs []string) []string {
	if dirs == nil {
		return []string{}
	}
	return dirs
}

func (a *Agent) workspaceRoot() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.svc.workspaceRoot)
}

func (a *Agent) homeDir() string {
	if a != nil && a.svc.homeDir != "" {
		return a.svc.homeDir
	}
	home, _ := os.UserHomeDir()
	return home
}

func (a *Agent) stateRoot() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.svc.stateRoot)
}

func (a *Agent) stampWriteRoots(ctx context.Context, plan *toolCallPlan) context.Context {
	if len(plan.perCallWriteRoots) > 0 {
		ctx = sandbox.WithPerCallWriteRoots(ctx, plan.perCallWriteRoots)
	}
	return ctx
}

// BindChildWriteRoots rebinds writer/bash tools onto a snapshot of the parent
// set (or the write_paths intersection). Later parent grants cannot expand the
// child. A whole-workspace or empty claim inherits the current snapshot.
func BindChildWriteRoots(reg *tool.Registry, parent *sandbox.WritableRootSet, claims WritePathSet) (*tool.Registry, *sandbox.WritableRootSet) {
	if parent == nil || reg == nil {
		return reg, parent
	}
	cap := claims.Roots()
	if claims.Empty() || claims.WholeWorkspace {
		cap = nil
	}
	childSet := parent.CloneRestricted(cap)
	for _, name := range reg.Names() {
		tl, ok := reg.Get(name)
		if !ok {
			continue
		}
		reg.Add(bindToolWriteRootSet(tl, childSet))
	}
	return reg, childSet
}

func bindToolWriteRootSet(tl tool.Tool, set *sandbox.WritableRootSet) tool.Tool {
	switch t := tl.(type) {
	case foregroundOnlyBash:
		t.inner = builtin.BindWriteRootSet(t.inner, set)
		return t
	case readOnlyBash:
		t.inner = builtin.BindWriteRootSet(t.inner, set)
		return t
	case pathBoundWriter:
		t.inner = builtin.BindWriteRootSet(t.inner, set)
		return t
	default:
		return builtin.BindWriteRootSet(tl, set)
	}
}
