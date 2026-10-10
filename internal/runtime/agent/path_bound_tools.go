package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/runtime/writeclaim"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/permission"
	"reasonix/internal/tools/builtin"
)

// pathBoundWriter wraps a built-in write tool so each Execute stays inside a
// declared WritePathSet. Unknown/custom/MCP writers that cannot be path-scoped
// are dropped from the parallel-writer registry instead (see BindWritePaths).
type pathBoundWriter struct {
	inner   tool.Tool
	grant   *writeclaim.WriteGrant
	gate    Gate
	sched   *writeclaim.SubagentScheduler
	workDir string
}

// pathBoundCapabilityProxy preserves the provider-visible use_capability
// contract while enforcing an explicit write_paths boundary after dynamic
// resolution. Discovery stays available, but a call must resolve to a proven
// read-only, non-destructive target before any MCP process or tool executes.
type pathBoundCapabilityProxy struct {
	inner    tool.Tool
	resolver tool.CallResolver
}

func (p pathBoundCapabilityProxy) Name() string            { return p.inner.Name() }
func (p pathBoundCapabilityProxy) Description() string     { return p.inner.Description() }
func (p pathBoundCapabilityProxy) Schema() json.RawMessage { return p.inner.Schema() }
func (p pathBoundCapabilityProxy) ReadOnly() bool          { return p.inner.ReadOnly() }

func (p pathBoundCapabilityProxy) ResolveCall(ctx context.Context, args json.RawMessage) (tool.ResolvedCall, error) {
	resolved, err := p.resolver.ResolveCall(ctx, args)
	if err != nil {
		return tool.ResolvedCall{}, err
	}
	if resolved.ProxyAction != "call" || resolved.SkipExecute {
		return resolved, nil
	}
	if resolved.Target == nil || !resolved.ReadOnly || tool.HasMCPDestructiveHint(resolved.Target) {
		return tool.ResolvedCall{}, fmt.Errorf("use_capability target %q is not proven read-only; explicit write_paths sub-agents cannot execute unscoped MCP writers", resolved.TargetName)
	}
	return resolved, nil
}

func (p pathBoundCapabilityProxy) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	resolved, err := p.ResolveCall(ctx, args)
	if err != nil {
		return "", err
	}
	if resolved.Commit != nil {
		if err := resolved.Commit(); err != nil {
			return "", err
		}
	}
	if resolved.SkipExecute {
		return resolved.Result, nil
	}
	if resolved.Target == nil {
		return "", fmt.Errorf("use_capability resolved no target")
	}
	return resolved.Target.Execute(ctx, resolved.Args)
}

func (w pathBoundWriter) Name() string            { return w.inner.Name() }
func (w pathBoundWriter) Description() string     { return w.inner.Description() }
func (w pathBoundWriter) Schema() json.RawMessage { return w.inner.Schema() }
func (w pathBoundWriter) ReadOnly() bool          { return w.inner.ReadOnly() }
func (w pathBoundWriter) WritesNamedPaths() bool  { return tool.WritesNamedPaths(w.inner) }
func (w pathBoundWriter) PlanModeSafe() bool {
	if p, ok := w.inner.(interface{ PlanModeSafe() bool }); ok {
		return p.PlanModeSafe()
	}
	return false
}

// Execute confines the write to this run's grant. A path outside it is a
// question rather than a refusal — what a run must touch is not always knowable
// before it reads anything — and the question goes to the user, never to the
// parent: a model approving its own reach is not a fence. Paths are the
// resolved ones, so a symlink names the file the write would really land on.
func (w pathBoundWriter) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	paths, err := w.WritePaths(args)
	if err != nil && (!errors.Is(err, fileutil.ErrAmbiguousPath) || len(paths) == 0) {
		return "", err
	}
	declared := w.grant.Declared()
	var held []func()
	defer func() {
		for _, release := range held {
			release()
		}
	}()
	for _, p := range paths {
		// Inside what was declared: the scheduler proved non-overlap before
		// anything started, and there is nothing left to ask or to reserve.
		if declared.AllowsPath(p) {
			continue
		}
		if !w.grant.Allows(p) {
			if err := w.askToWiden(ctx, p); err != nil {
				return "", err
			}
		}
		// Granted or not, every write outside the declared set reserves: a
		// grant says this run may be here, never that another may not.
		release, err := w.sched.ReserveWrite(writeclaim.WritePathSet{Paths: []string{p}})
		if err != nil {
			return "", fmt.Errorf("%w: %s", writeclaim.ErrWritePathBusy, p)
		}
		held = append(held, release)
	}
	return w.inner.Execute(ctx, args)
}

func (w pathBoundWriter) WritePaths(args json.RawMessage) ([]string, error) {
	if writer, ok := w.inner.(tool.WritePathResolver); ok {
		return writer.WritePaths(args)
	}
	return extractWritePathsFromArgs(w.inner.Name(), w.workDir, args)
}

func (w pathBoundWriter) ReadPaths(ctx context.Context, args json.RawMessage) ([]string, error) {
	if reader, ok := w.inner.(tool.ReadPathResolver); ok {
		return reader.ReadPaths(ctx, args)
	}
	return w.WritePaths(args)
}

// askToWiden puts one path to the user and records the answer. With no gate
// there is nobody to ask, and an unanswerable question is a refusal: a run that
// could widen its own fence whenever the host happened to have no approver
// would not be confined at all.
func (w pathBoundWriter) askToWiden(ctx context.Context, path string) error {
	if w.gate == nil {
		return fmt.Errorf("%w: %s", writeclaim.ErrWriteFenceClosed, path)
	}
	subject, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return err
	}
	allow, reason, err := w.gate.Check(ctx, permission.ExtendWritePaths, subject, false)
	if err != nil {
		return err
	}
	if !allow {
		if reason = strings.TrimSpace(reason); reason != "" {
			return fmt.Errorf("%w: %s (%s)", writeclaim.ErrWriteFenceClosed, path, reason)
		}
		return fmt.Errorf("%w: %s", writeclaim.ErrWriteFenceClosed, path)
	}
	w.grant.Add(path)
	return nil
}

// pathBoundWriterNames are built-in tools whose arguments expose file paths we
// can enforce against write_paths claims.
var pathBoundWriterNames = map[string]bool{
	"write_file":    true,
	"edit_file":     true,
	"multi_edit":    true,
	"move_file":     true,
	"notebook_edit": true,
	"delete_range":  true,
	"delete_symbol": true,
}

// BindWritePaths returns a copy of reg where built-in writers are re-bound to
// the claim and non-path-scoped writer tools (MCP/custom) are dropped.
// Bash is kept only when keepBash is true AND its OS sandbox WriteRoots can be
// re-bound to the claim roots; otherwise bash is removed.
func BindWritePaths(reg *tool.Registry, grant *writeclaim.WriteGrant, gate Gate, sched *writeclaim.SubagentScheduler, workDir string, keepBash bool) (bound *tool.Registry, removed []string) {
	bound = tool.NewRegistry()
	if reg == nil {
		return bound, nil
	}
	bound.CopyDisabledMCPFrom(reg)
	claims := grant.Declared()
	if claims.Empty() {
		for _, name := range reg.Names() {
			if tl, ok := reg.Get(name); ok {
				bound.Add(tl)
			}
		}
		return bound, nil
	}
	roots := claims.Roots()
	for _, name := range reg.Names() {
		tl, ok := reg.Get(name)
		if !ok {
			continue
		}
		if name == "bash" {
			if !keepBash {
				removed = append(removed, name)
				continue
			}
			rebound, ok := rebindBashToClaimRoots(tl, roots)
			if !ok {
				removed = append(removed, name)
				continue
			}
			bound.Add(rebound)
			continue
		}
		if name == "use_capability" {
			resolver, ok := tl.(tool.CallResolver)
			if !ok {
				removed = append(removed, name)
				continue
			}
			bound.Add(pathBoundCapabilityProxy{inner: tl, resolver: resolver})
			continue
		}
		if tl.ReadOnly() {
			bound.Add(tl)
			continue
		}
		if pathBoundWriterNames[name] {
			bound.Add(pathBoundWriter{inner: tl, grant: grant, gate: gate, sched: sched, workDir: workDir})
			continue
		}
		// MCP / custom writers cannot be path-scoped reliably.
		removed = append(removed, name)
	}
	return bound, removed
}

// rebindBashToClaimRoots rebinds a bash tool (or foregroundOnlyBash wrapper)
// so OS sandbox WriteRoots equal the claim roots.
func rebindBashToClaimRoots(tl tool.Tool, roots []string) (tool.Tool, bool) {
	if len(roots) == 0 {
		return nil, false
	}
	if fb, ok := tl.(foregroundOnlyBash); ok {
		rebound, ok := builtin.RebindBashWriteRoots(fb.inner, roots)
		if !ok {
			return nil, false
		}
		return foregroundOnlyBash{inner: rebound}, true
	}
	return builtin.RebindBashWriteRoots(tl, roots)
}

// parentWriteGuardTarget reports tools whose parent-side execution can mutate
// workspace files and must reserve write claims for the duration of Execute.
// Meta/delegation tools (task, fleet, run_skill, …) are excluded so the parent
// can still schedule while background writers run.
func parentWriteGuardTarget(name string) bool {
	if pathBoundWriterNames[name] || name == "bash" {
		return true
	}
	return strings.HasPrefix(name, tool.MCPNamePrefix)
}

// parentWriteReservation builds the WritePathSet a parent tool must hold while
// executing. Path-aware built-ins reserve concrete targets; bash/MCP reserve
// the whole workspace (targets cannot be judged reliably).
func parentWriteReservation(workDir, toolName string, args json.RawMessage) (writeclaim.WritePathSet, error) {
	if pathBoundWriterNames[toolName] {
		paths, err := extractWritePathsFromArgs(toolName, workDir, args)
		if err != nil {
			return writeclaim.WritePathSet{}, fmt.Errorf("could not parse %s path for write reservation: %w", toolName, err)
		}
		return parentResolvedWriteReservation(workDir, paths)
	}
	// Bash and MCP/custom writers.
	return writeclaim.WholeWorkspaceWriteClaim(workDir)
}

func extractWritePathsFromArgs(toolName, workDir string, args json.RawMessage) ([]string, error) {
	return builtin.ResolveWritePaths(workDir, nil, []string{workDir}, args, toolName == "move_file")
}

func parentResolvedWriteReservation(workDir string, paths []string) (writeclaim.WritePathSet, error) {
	if len(paths) == 0 {
		return writeclaim.WholeWorkspaceWriteClaim(workDir)
	}
	set, err := writeclaim.NormalizeWritePaths(workDir, paths)
	if err != nil {
		return writeclaim.WholeWorkspaceWriteClaim(workDir)
	}
	return set, nil
}
