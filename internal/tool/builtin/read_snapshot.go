package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"reasonix/internal/tool"
)

// Full/range reads may capture a bounded source for version-safe paging. A
// preview never scans a large file merely to establish a whole-file identity.
const maxReadSnapshotBytes = 64 << 20

func (r readFile) ResolveReadPath(args json.RawMessage) (string, error) {
	// Path identity must remain available even when another argument is
	// invalid, so a failed continuation still belongs to its bounded task.
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	return resolveReadablePath(r.workDir, p.Path, r.paths).Path, nil
}

func (r readFile) ExecuteRead(ctx context.Context, args json.RawMessage) (string, tool.ReadResultEnvelope, error) {
	p, err := parseReadFileParams(args)
	if err != nil {
		return "", tool.ReadResultEnvelope{}, err
	}
	rp := resolveReadablePath(r.workDir, p.Path, r.paths)
	if confineRead(r.forbidRoots, rp.Path) {
		return "", tool.ReadResultEnvelope{}, &os.PathError{Op: "open", Path: rp.DisplayPath, Err: os.ErrNotExist}
	}
	source := tool.ReadResultSource{CanonicalPath: rp.Path}
	r.captured = &source
	var output string
	if content, ok := r.overlayText(ctx, rp); ok {
		source.Kind = tool.ReadSourceOverlay
		source.Identity = "overlay:" + digestText(content)
		// Both output and identity derive from this exact buffer instance.
		output, err = r.scan(readContextReader{ctx, strings.NewReader(content)}, p.Offset, p.Limit)
	} else {
		source.Kind = tool.ReadSourceDisk
		limit := int64(maxReadSnapshotBytes)
		if p.Intent != tool.ReadIntentFull && !tool.FullReadSnapshotRequested(ctx) {
			limit = readFileDetectSample
		}
		f, openErr := os.Open(rp.Path)
		if openErr != nil {
			return "", tool.ReadResultEnvelope{}, fmt.Errorf("read %s: %s", rp.DisplayPath, rp.ErrorText(openErr))
		}
		info, statErr := f.Stat()
		if statErr == nil && info.Mode().IsRegular() && info.Size() <= limit {
			raw, readErr := io.ReadAll(io.LimitReader(readContextReader{ctx, f}, limit+1))
			if readErr != nil {
				_ = f.Close()
				return "", tool.ReadResultEnvelope{}, readErr
			}
			if int64(len(raw)) <= limit {
				r.rawSnapshot = raw
				source.Identity = "raw-sha256:" + digestText(string(raw))
			}
		}
		_ = f.Close()
		output, err = r.Execute(ctx, args)
	}
	source.Snapshot = tool.SourceSnapshot(source.Kind, source.CanonicalPath, source.Identity)
	env, _ := r.ReadEnvelope(ctx, args, output)
	return output, env, err
}

type readContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r readContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
