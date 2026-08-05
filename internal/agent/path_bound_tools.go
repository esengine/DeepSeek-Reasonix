package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

// pathBoundWriter wraps a built-in write tool so each Execute stays inside a
// declared WritePathSet. Unknown/custom/MCP writers that cannot be path-scoped
// are dropped from the parallel-writer registry instead (see BindWritePaths).
type pathBoundWriter struct {
	inner   tool.Tool
	claims  WritePathSet
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
	if resolved.Target == nil || !resolved.ReadOnly || mcpDestructiveHint(resolved.Target) {
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
func (w pathBoundWriter) PlanModeSafe() bool {
	if p, ok := w.inner.(interface{ PlanModeSafe() bool }); ok {
		return p.PlanModeSafe()
	}
	return false
}

func (w pathBoundWriter) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	paths, err := extractWritePathsFromArgs(w.inner.Name(), w.workDir, args)
	if err != nil {
		return "", err
	}
	for _, p := range paths {
		if !w.claims.AllowsPath(p) {
			return "", fmt.Errorf("write path %q is outside this subagent's declared write_paths", p)
		}
	}
	return w.inner.Execute(ctx, args)
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
func BindWritePaths(reg *tool.Registry, claims WritePathSet, workDir string, keepBash bool) (bound *tool.Registry, removed []string) {
	bound = tool.NewRegistry()
	if reg == nil {
		return bound, nil
	}
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
			bound.Add(pathBoundWriter{inner: tl, claims: claims, workDir: workDir})
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
func parentWriteReservation(workDir, toolName string, args json.RawMessage) (WritePathSet, error) {
	if pathBoundWriterNames[toolName] {
		paths, err := extractWritePathsFromArgs(toolName, workDir, args)
		if err != nil {
			return WritePathSet{}, fmt.Errorf("could not parse %s path for write reservation: %w", toolName, err)
		}
		// NormalizeWritePaths accepts relative paths against the workspace.
		// Absolute paths already inside the workspace also work.
		raw := make([]string, 0, len(paths))
		for _, p := range paths {
			raw = append(raw, resolveMaybeRelative(workDir, p))
		}
		set, err := NormalizeWritePaths(workDir, raw)
		if err != nil {
			// Outside workspace: still take a whole-workspace reservation so we
			// cannot race background writers while writing managed paths outside
			// roots (config write approval path).
			whole, werr := WholeWorkspaceWriteClaim(workDir)
			if werr != nil {
				return WritePathSet{}, err
			}
			return whole, nil
		}
		return set, nil
	}
	// Bash and MCP/custom writers.
	return WholeWorkspaceWriteClaim(workDir)
}

func extractWritePathsFromArgs(toolName, workDir string, args json.RawMessage) ([]string, error) {
	switch toolName {
	case "move_file":
		var p struct {
			SourcePath      string `json:"source_path"`
			DestinationPath string `json:"destination_path"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if strings.TrimSpace(p.SourcePath) == "" || strings.TrimSpace(p.DestinationPath) == "" {
			return nil, fmt.Errorf("source_path and destination_path are required")
		}
		return []string{
			resolveMaybeRelative(workDir, p.SourcePath),
			resolveMaybeRelative(workDir, p.DestinationPath),
		}, nil
	default:
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if strings.TrimSpace(p.Path) == "" {
			return nil, fmt.Errorf("path is required")
		}
		return []string{resolveMaybeRelative(workDir, p.Path)}, nil
	}
}

func resolveMaybeRelative(workDir, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(workDir) == "" {
		return path
	}
	return filepath.Join(workDir, path)
}
