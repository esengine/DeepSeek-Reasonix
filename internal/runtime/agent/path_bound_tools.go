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
		if declared.AllowsPath(p) && !declared.Optimistic {
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
			return "", fmt.Errorf("%w: %s", err, p)
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

func (w pathBoundWriter) FormatReadRefusal(err error) error {
	if formatter, ok := w.inner.(tool.ReadRefusalFormatter); ok {
		return formatter.FormatReadRefusal(err)
	}
	return err
}
