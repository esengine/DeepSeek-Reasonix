package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"reasonix/internal/base/workspacefile"
	"reasonix/internal/contract/delivery"
	"reasonix/internal/contract/tool"
)

func init() { tool.RegisterConditional(presentFiles{}) }

// PresentFiles binds the declaration tool to the same file policy as reads.
func (w Workspace) PresentFiles() tool.Tool {
	readRoots := realRoots(w.ReadRoots)
	if len(w.ReadRoots) > 0 && len(readRoots) == 0 {
		readRoots = []string{filepath.Join(string(filepath.Separator), "\x00unresolved")}
	}
	return presentFiles{workDir: w.Dir, roots: w.WriteRoots, readRoots: readRoots, forbidRoots: realRoots(w.ForbidReadRoots)}
}

type presentFiles struct {
	workDir     string
	roots       []string
	readRoots   []string
	forbidRoots []string
}

func (presentFiles) Name() string { return delivery.ToolName }
func (presentFiles) Description() string {
	return "Present existing final files as clickable cards, including shell-created documents. Paths must be in the workspace or authorized folders. Opens current files, not snapshots; does not certify correctness."
}
func (presentFiles) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"files":{"type":"array","minItems":1,"maxItems":4,"items":{"type":"object","properties":{"path":{"type":"string","maxLength":4096},"description":{"type":"string","maxLength":256}},"required":["path"],"additionalProperties":false}}},"required":["files"],"additionalProperties":false}`)
}
func (presentFiles) ReadOnly() bool    { return true }
func (presentFiles) FilePresentation() {}
func (presentFiles) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 1, Tail: 1, HeadChars: 32 << 10, TailChars: 1}
}

func (p presentFiles) ReadPaths(_ context.Context, args json.RawMessage) ([]string, error) {
	var in struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	var paths []string
	for _, file := range in.Files {
		path := file.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.workDir, path)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (p presentFiles) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Files []struct {
			Path        string `json:"path"`
			Description string `json:"description"`
		} `json:"files"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if len(in.Files) == 0 || len(in.Files) > delivery.MaxFiles {
		return "", fmt.Errorf("present_files requires 1 to 4 files")
	}
	root, err := filepath.Abs(p.workDir)
	if err != nil {
		return "", err
	}
	service := workspacefile.Service{Primary: root, Extra: p.roots}
	result := delivery.Result{Version: 1, Files: make([]delivery.File, 0, len(in.Files))}
	for _, file := range in.Files {
		if len(file.Path) > 4096 || len(file.Description) > 1024 {
			return "", fmt.Errorf("file path or description is too long")
		}
		ref, err := service.Resolve(file.Path)
		if err != nil {
			return "", err
		}
		base := root
		if ref.Root != "" {
			base = ref.Root
		}
		target, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(ref.Path)))
		if err != nil {
			return "", err
		}
		if err := checkReadTargetAccess(p.readRoots, p.forbidRoots, target); err != nil {
			return "", err
		}
		if _, err := service.Info(ctx, ref); err != nil {
			return "", err
		}
		result.Files = append(result.Files, delivery.File{Path: ref.Path, Root: ref.Root, Description: file.Description})
	}
	data, err := json.Marshal(result)
	if len(data) > 32<<10 {
		return "", fmt.Errorf("file declaration exceeds 32 KiB")
	}
	return string(data), err
}
