package builtin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/tool"
)

const (
	ArtifactDownloadName        = "download_artifact"
	CodeArtifactTooLarge        = "artifact.too_large"
	CodeArtifactTargetExists    = "artifact.target_exists"
	CodeArtifactRedirectRefused = "artifact.redirect_refused"
	artifactDownloadTimeout     = 10 * time.Minute
)

func init() { tool.RegisterConditional(artifactDownload{}) }

// ArtifactDownload binds download_artifact to the workspace's writer roots.
func (w Workspace) ArtifactDownload() tool.Tool {
	return artifactDownload{workDir: w.Dir, roots: w.writerRoots(), guard: w.SessionGuard, proxySpec: w.ProxySpec, receipt: w.FileWriteReceipt}
}

type artifactDownload struct {
	workDir   string
	roots     []string
	guard     SessionDataGuard
	proxySpec netclient.ProxySpec
	receipt   func(string, bool, []byte)
}

type artifactDownloadArgs struct {
	Source   string `json:"source"`
	Pointer  string `json:"pointer"`
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type artifactReceipt struct {
	Version        int    `json:"version"`
	Path           string `json:"path"`
	ProvenancePath string `json:"provenance_path"`
	SHA256         string `json:"sha256"`
	Bytes          int64  `json:"bytes"`
}

// artifactSourceReceipt is portable: the signed address stays in the private result sidecar.
type artifactSourceReceipt struct {
	artifactReceipt
	Source        string    `json:"source"`
	Pointer       string    `json:"pointer"`
	Origin        string    `json:"origin"`
	RedirectHosts []string  `json:"redirect_hosts"`
	DownloadedAt  time.Time `json:"downloaded_at"`
}

func (artifactDownload) Name() string           { return ArtifactDownloadName }
func (artifactDownload) ReadOnly() bool         { return false }
func (artifactDownload) WritesNamedPaths() bool { return true }
func (artifactDownload) Description() string {
	return "Download a video URL selected by JSON pointer from a retained session tool-result. Saves a new workspace file and private source receipt; never overwrites."
}
func (artifactDownload) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["source","pointer","path"],"properties":{"source":{"type":"string","description":"tool-result: reference"},"pointer":{"type":"string","description":"JSON pointer relative to result, e.g. /structuredContent/video_url or /content/1/uri"},"path":{"type":"string","description":"new workspace-relative video path"},"max_bytes":{"type":"integer","minimum":1,"maximum":4294967296}}}`)
}
func (artifactDownload) Provenance(json.RawMessage) tool.Provenance {
	return tool.Provenance{Kind: tool.ProvenanceMCP, Source: "tool-result"}
}
func (w artifactDownload) WritePaths(args json.RawMessage) ([]string, error) {
	var p artifactDownloadArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	paths, err := ResolveWritePaths(w.workDir, nil, w.roots, args, false)
	if err != nil {
		return paths, err
	}
	sourceArgs, _ := json.Marshal(map[string]string{"path": p.Path + ".source.json"})
	more, err := ResolveWritePaths(w.workDir, nil, w.roots, sourceArgs, false)
	return append(paths, more...), err
}
func (artifactDownload) ReadPaths(context.Context, json.RawMessage) ([]string, error) {
	return nil, nil
}

func (w artifactDownload) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p artifactDownloadArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if err := validateArtifactTarget(p.Path); err != nil {
		return "", err
	}
	if p.MaxBytes == 0 {
		p.MaxBytes = readVideoMaxBytes
	}
	if p.MaxBytes < 1 || p.MaxBytes > readVideoMaxBytes {
		return "", fmt.Errorf("max_bytes must be between 1 and %d", readVideoMaxBytes)
	}
	for _, path := range []string{p.Path, p.Path + ".source.json"} {
		if err := confineWrite(ctx, w.roots, w.guard, ManagedConfigPaths{}, nil, resolveIn(w.workDir, path)); err != nil {
			return "", err
		}
	}
	u, err := artifactSourceURL(ctx, p.Source, p.Pointer)
	if err != nil {
		return "", err
	}
	dir := w.workDir
	if dir == "" {
		dir = "."
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	for _, path := range []string{p.Path, p.Path + ".source.json"} {
		if _, err := root.Lstat(path); err == nil {
			return "", artifactExistsError(path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, artifactDownloadTimeout)
	defer cancel()
	resp, redirectHosts, err := w.fetch(ctx, u.String())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.ContentLength > p.MaxBytes {
		return "", artifactSizeError(p.MaxBytes)
	}
	if err := root.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return "", err
	}
	stage := filepath.Join(filepath.Dir(p.Path), ".reasonix-artifact-"+rand.Text())
	f, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Remove(stage) }()
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, p.MaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("download body: %w", err)
	}
	if n > p.MaxBytes {
		return "", artifactSizeError(p.MaxBytes)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	head := make([]byte, readVideoSniffBytes)
	nh, _ := f.ReadAt(head, 0)
	if videoDemuxer(head[:nh]) == "" {
		return "", tool.Refusal{Code: CodeVideoUnsupported, Message: "download is not a supported video container"}
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	receipt := artifactReceipt{Version: 1, Path: p.Path, ProvenancePath: p.Path + ".source.json", SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: n}
	source := artifactSourceReceipt{artifactReceipt: receipt, Source: p.Source, Pointer: p.Pointer, Origin: u.Scheme + "://" + u.Host, RedirectHosts: redirectHosts, DownloadedAt: time.Now().UTC()}
	if err := publishArtifact(root, stage, source); err != nil {
		return "", err
	}
	if w.receipt != nil {
		w.receipt(resolveIn(w.workDir, p.Path), false, nil)
		w.receipt(resolveIn(w.workDir, receipt.ProvenancePath), false, nil)
	}
	out, err := json.Marshal(receipt)
	return string(out), err
}

func validateArtifactTarget(path string) error {
	if !filepath.IsLocal(path) || path == "." || len(path) > 1024 || strings.ContainsAny(path, ":\\?#%\x00\r\n\t") || filepath.Clean(path) != path {
		return fmt.Errorf("new workspace-relative video path required")
	}
	return nil
}

func artifactExistsError(path string) error {
	return tool.Refusal{Code: CodeArtifactTargetExists, Message: path + " already exists; choose a new path, artifacts are never overwritten"}
}

func artifactSizeError(limit int64) error {
	return tool.Refusal{Code: CodeArtifactTooLarge, Message: fmt.Sprintf("artifact exceeds %d bytes", limit)}
}
