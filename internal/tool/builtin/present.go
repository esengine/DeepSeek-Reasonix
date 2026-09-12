package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/tool"
)

type presentFile struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

type presentParams struct {
	Files []presentFile `json:"files"`
}

// present declares existing files as user-facing deliverables. It reads only
// file metadata; the desktop/remote host resolves the recorded resource again
// when the user opens it.
type present struct {
	workDir     string
	paths       *PathResolver
	forbidRoots []string
}

func init() { tool.RegisterBuiltin(present{}) }

func (present) Name() string { return "present" }

func (present) Description() string {
	return "Present files that were generated or updated as user-facing deliverables. Call this after writing the files and before the final answer, including files created through bash or code execution. Mentioning a path only in the answer does not replace this call. In the final answer, refer to each presented file by its exact path or unique basename in inline code so the host can attach its open action. Each path must identify an existing regular file accessible to this session; accepts 1 to 8 files."
}

func (present) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"files":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"object","properties":{"path":{"type":"string","minLength":1,"description":"Existing file path, relative to the session workspace or an authorized absolute path"},"description":{"type":"string","description":"Short user-facing description of the file"}},"required":["path"],"additionalProperties":false}}},"required":["files"],"additionalProperties":false}`)
}

func (present) ReadOnly() bool     { return true }
func (present) PlanModeSafe() bool { return false }

func (present) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 16, Tail: 2, HeadChars: 4000, TailChars: 500}
}

func (p present) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var params presentParams
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if len(params.Files) < 1 || len(params.Files) > 8 {
		return "", fmt.Errorf("files must contain between 1 and 8 entries")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	validated := make([]tool.PresentedFile, 0, len(params.Files))
	for index, file := range params.Files {
		path := strings.TrimSpace(file.Path)
		if path == "" {
			return "", fmt.Errorf("files[%d].path is required", index)
		}
		rp := resolveReadablePath(p.workDir, path, p.paths)
		// Resolve the deny roots and the candidate at the same boundary. This is
		// required on macOS where /var and /private/var may name the same bytes,
		// and prevents a symlinked parent from bypassing a configured deny root.
		if ReadPathForbidden(p.forbidRoots, rp.Path) {
			return "", fmt.Errorf("present %s: file not found", rp.DisplayPath)
		}
		info, err := os.Lstat(rp.Path)
		if err != nil {
			return "", fmt.Errorf("present %s: %s", rp.DisplayPath, rp.ErrorText(err))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("present %s: final path must not be a symbolic link", rp.DisplayPath)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("present %s: path is not a regular file", rp.DisplayPath)
		}
		var recordedPath string
		if filepath.IsAbs(path) {
			recordedPath = filepath.Clean(path)
		} else {
			recordedPath = filepath.ToSlash(filepath.Clean(path))
		}
		validated = append(validated, tool.PresentedFile{
			Path: recordedPath, Description: strings.TrimSpace(file.Description),
		})
	}

	tool.RecordPresentedFiles(ctx, validated)
	var result strings.Builder
	for _, file := range validated {
		fmt.Fprintf(&result, "Presented %s\n", file.Path)
	}
	return strings.TrimSuffix(result.String(), "\n"), nil
}
