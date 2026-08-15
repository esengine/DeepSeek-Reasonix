package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func effectiveWriteRoots(ctx context.Context, set *sandbox.WritableRootSet, fallback []string) []string {
	if set != nil {
		return set.Effective(ctx)
	}
	if extra := sandbox.PerCallWriteRoots(ctx); len(extra) > 0 {
		return sandbox.CollapseWriteRoots(append(append([]string{}, fallback...), extra...))
	}
	return fallback
}

func declareParentWriteDirs(workDir string, paths ...string) (tool.WriteAccessDeclaration, error) {
	var dirs []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			return tool.WriteAccessDeclaration{}, fmt.Errorf("path is required")
		}
		resolved := resolveIn(workDir, p)
		dir := filepath.Dir(resolved)
		if dir == "" || dir == "." {
			continue
		}
		dirs = append(dirs, dir)
	}
	return tool.WriteAccessDeclaration{Directories: dirs}, nil
}

func declareFilePathWriteAccess(workDir string, args json.RawMessage) (tool.WriteAccessDeclaration, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.WriteAccessDeclaration{}, fmt.Errorf("invalid args: %w", err)
	}
	return declareParentWriteDirs(workDir, p.Path)
}

// BindWriteRootSet attaches a live writable-root manager to a built-in writer
// or bash tool so later session grants are visible without replacing the registry.
func BindWriteRootSet(tl tool.Tool, set *sandbox.WritableRootSet) tool.Tool {
	if set == nil {
		return tl
	}
	switch t := tl.(type) {
	case writeFile:
		t.rootSet = set
		return t
	case editFile:
		t.rootSet = set
		return t
	case multiEdit:
		t.rootSet = set
		return t
	case moveFile:
		t.rootSet = set
		return t
	case notebookEdit:
		t.rootSet = set
		return t
	case deleteRange:
		t.rootSet = set
		return t
	case deleteSymbol:
		t.rootSet = set
		return t
	case bash:
		t.rootSet = set
		return t
	default:
		return tl
	}
}
