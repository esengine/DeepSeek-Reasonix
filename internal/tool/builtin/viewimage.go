package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"reasonix/internal/attachment"
	"reasonix/internal/tool"
)

// Bound encoded payload size consistently with MCP image results.
const viewImageMaxBytes = 3 << 20

type viewImage struct {
	workDir     string
	paths       *PathResolver
	forbidRoots []string
}

func init()                    { tool.RegisterBuiltin(viewImage{}) }
func (viewImage) Name() string { return "view_image" }
func (viewImage) Description() string {
	return "Read a local PNG, JPEG, GIF, or WebP image by path and return visual content through native vision or the configured image-understanding model. Use this for image paths instead of read_file. Maximum file size: 3 MiB; maximum dimensions: 40 million pixels."
}
func (viewImage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Image file path"}},"required":["path"]}`)
}
func (viewImage) ReadOnly() bool { return true }
func (v viewImage) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, _, err := v.ExecuteWithImages(ctx, args)
	if err == nil {
		text += "\nVisual content requires a structured image channel and an image-capable model."
	}
	return text, err
}
func (v viewImage) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", nil, fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Path) == "" {
		return "", nil, fmt.Errorf("path is required")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	rp := resolveReadablePath(v.workDir, p.Path, v.paths)
	if confineRead(v.forbidRoots, rp.Path) {
		return "", nil, fmt.Errorf("read %s: file not found", rp.DisplayPath)
	}
	info, err := os.Stat(rp.Path)
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %s", rp.DisplayPath, rp.ErrorText(err))
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%s is not a regular image file", rp.DisplayPath)
	}
	if info.Size() > viewImageMaxBytes {
		return "", nil, fmt.Errorf("image exceeds 3 MiB limit")
	}
	f, err := os.Open(rp.Path)
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %s", rp.DisplayPath, rp.ErrorText(err))
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, viewImageMaxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %s", rp.DisplayPath, rp.ErrorText(err))
	}
	if len(data) > viewImageMaxBytes {
		return "", nil, fmt.Errorf("image exceeds 3 MiB limit")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	mime, width, height, err := attachment.ValidateImage(data, "", attachment.ViewImagePolicy())
	if err != nil {
		return "", nil, fmt.Errorf("invalid or unsupported image: %w", err)
	}
	return fmt.Sprintf("[image: %s, %dx%d] %s", mime, width, height, rp.DisplayPath), []string{"data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
}
