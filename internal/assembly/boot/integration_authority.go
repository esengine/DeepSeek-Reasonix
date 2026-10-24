package boot

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reasonix/internal/base/fileutil"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/session/control"
)

// integrationAuthorizer retains live runtime authority, never a persisted
// authorized=true flag. The grant is the source grant, before worktree mapping.
func integrationAuthorizer(source, owner string, posture *isolationPosture, gate *control.SharedHeadlessGate, access tool.TargetAccessCheck, grant writeclaim.WritePathSet) isolation.SourceAuthorizer {
	controller := posture.ctrl.Load()
	validateExecution := controller.MutationExecutionValidator(owner)
	return integrationAuthorizerWithExecution(source, posture, gate, access, grant, validateExecution)
}

func integrationAuthorizerWithExecution(source string, posture *isolationPosture, gate *control.SharedHeadlessGate, access tool.TargetAccessCheck, grant writeclaim.WritePathSet, validateExecution func(context.Context) error) isolation.SourceAuthorizer {
	controller := posture.ctrl.Load()
	return func(ctx context.Context, paths []string) error {
		if controller == nil || validateExecution == nil || posture.ctrl.Load() != controller {
			return tool.Refusal{Code: isolation.CodeStaleExecution, Message: "source runtime generation changed"}
		}
		if err := validateExecution(ctx); err != nil {
			return err
		}
		canonicalSource, err := fileutil.CanonicalWritePath(source)
		if err != nil {
			return err
		}
		for _, path := range paths {
			canonical, err := fileutil.CanonicalWritePath(path)
			if err != nil {
				return err
			}
			if !grant.AllowsPath(canonical) {
				return tool.Refusal{Code: isolation.CodeOwnerMismatch, Message: "integration path exceeds original source write grant"}
			}
			relative, err := filepath.Rel(canonicalSource, canonical)
			if err != nil || !filepath.IsLocal(relative) {
				return tool.Refusal{Code: isolation.CodeOwnerMismatch, Message: "integration path is outside the source workspace"}
			}
		}
		return checkSourceMutationPaths(ctx, source, gate, access, paths)
	}
}

func checkSourceMutationPaths(ctx context.Context, source string, gate *control.SharedHeadlessGate, access tool.TargetAccessCheck, paths []string) error {
	if gate == nil || access == nil || gate.DeniesWriters() {
		return tool.Refusal{Code: isolation.CodeOwnerMismatch, Message: "source write authorization is unavailable or read-only"}
	}
	for _, path := range paths {
		if integrationPathDenied(gate, source, path) {
			return tool.Refusal{Code: isolation.CodeOwnerMismatch, Message: "current source policy denies integration"}
		}
		args, _ := json.Marshal(map[string]string{"path": filepath.ToSlash(path)})
		if err := access(ctx, integrationTarget{path: path}, args); err != nil {
			return err
		}
	}
	return nil
}

func integrationPathDenied(gate *control.SharedHeadlessGate, source, path string) bool {
	names := []string{path}
	canonicalSource, err := fileutil.CanonicalWritePath(source)
	if err != nil {
		return true
	}
	canonicalPath, err := fileutil.CanonicalWritePath(path)
	if err != nil {
		return true
	}
	names = append(names, canonicalPath)
	if relative, err := filepath.Rel(canonicalSource, canonicalPath); err == nil && filepath.IsLocal(relative) {
		names = append(names, relative, filepath.Join(source, relative))
	}
	for _, name := range names {
		args, _ := json.Marshal(map[string]string{"path": filepath.ToSlash(name)})
		for _, toolName := range []string{"apply_isolated", "write_file", "edit_file"} {
			if gate.ExplicitlyDenies(toolName, args) {
				return true
			}
		}
	}
	return false
}

func (b *builder) integrationRecoveryAccess(ctx context.Context, paths []string) error {
	return checkSourceMutationPaths(ctx, b.root, b.tools.gate, b.tools.env.targetAccessCheck(), paths)
}

type integrationTarget struct{ path string }

func (integrationTarget) Name() string             { return "apply_isolated" }
func (integrationTarget) Description() string      { return "Apply a verified isolated result" }
func (integrationTarget) Schema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (integrationTarget) ReadOnly() bool           { return false }
func (integrationTarget) WritesNamedPaths() bool   { return true }
func (integrationTarget) RequiresWriteScope() bool { return true }
func (t integrationTarget) WritePaths(json.RawMessage) ([]string, error) {
	return []string{t.path}, nil
}
func (t integrationTarget) ReadPaths(context.Context, json.RawMessage) ([]string, error) {
	return []string{t.path}, nil
}
func (integrationTarget) Execute(context.Context, json.RawMessage) (string, error) {
	panic("integration authorization target is never executable")
}
