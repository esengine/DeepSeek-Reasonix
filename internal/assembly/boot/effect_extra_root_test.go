package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type extraRootProvider struct {
	mu    sync.Mutex
	paths []string
	reqs  []provider.Request
}

func (p *extraRootProvider) Name() string { return "boot-effect-extra-root" }

func (p *extraRootProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	round := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if round <= len(p.paths) {
		args, _ := json.Marshal(map[string]string{"path": p.paths[round-1], "content": "x"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "w" + string(rune('0'+round)), Name: "write_file", Arguments: string(args)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func extraRootRun(t *testing.T, kind string, grant, swapForSSH bool) (reqs []provider.Request, granted, outside string) {
	t.Helper()
	home := isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	granted, outside = extraRootFolder(t), robustTempDir(t)
	rec := &extraRootProvider{paths: []string{filepath.Join(granted, "in.txt"), filepath.Join(outside, "out.txt")}}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() { ctrl.Close() }()
	if grant {
		if _, err := ctrl.AddWorkspaceRoot(granted); err != nil {
			t.Fatal(err)
		}
		ctrl.Close()
		if swapForSSH {
			ssh := filepath.Join(home, ".ssh")
			if err := os.MkdirAll(ssh, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(granted); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(ssh, granted); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}
		if ctrl, err = Build(context.Background(), Options{Sink: event.Discard}); err != nil {
			t.Fatalf("rebuild: %v", err)
		}
	}
	ctrl.SetBypass(true)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = ctrl.Run(ctx, "write the two files")
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]provider.Request(nil), rec.reqs...), granted, outside
}

// TestEffectGrantedWorkspaceRootIsWritableAndTheOtherIsNot pins the Integrity
// boundary at the real tool call: the granted folder takes the write, a folder
// nobody granted does not, and the cache-stable prefix does not move.
func TestEffectGrantedWorkspaceRootIsWritableAndTheOtherIsNot(t *testing.T) {
	with, granted, outside := extraRootRun(t, "boot-effect-root-a", true, false)
	if _, err := os.Stat(filepath.Join(granted, "in.txt")); err != nil {
		t.Fatalf("write into the granted root did not land: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "out.txt")); err == nil {
		t.Fatal("write into a folder nobody granted landed")
	}
	without, ungranted, _ := extraRootRun(t, "boot-effect-root-b", false, false)
	if _, err := os.Stat(filepath.Join(ungranted, "in.txt")); err == nil {
		t.Fatal("without the grant, the same write landed")
	}
	if len(with) == 0 || len(without) == 0 {
		t.Fatal("no request reached the provider")
	}
	if !strings.Contains(userMessages(with[0]), "Additional folders granted") || !strings.Contains(userMessages(with[0]), strconv.Quote(granted)) {
		t.Fatal("the model was not told which extra folders it may write")
	}
	if strings.Contains(userMessages(without[0]), "Additional folders") {
		t.Fatal("a workspace with no grants carried the extra-folder line")
	}
	if a, b := extraRootPrefix(with[0]), extraRootPrefix(without[0]); a != b {
		t.Fatalf("an extra root moved the cache-stable prefix: %s", firstDifference(a, b))
	}
}

func extraRootPrefix(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == provider.RoleSystem {
			b.WriteString(m.Content)
		}
	}
	for _, tl := range req.Tools {
		b.WriteString(tl.Name + string(tl.Parameters) + tl.Description)
	}
	return b.String()
}

// A granted folder replaced by a link to ~/.ssh is vetted again when the
// runtime is rebuilt, so the write that would plant a key does not land.
func TestEffectGrantedRootSwappedForSSHDirIsNotWritable(t *testing.T) {
	_, granted, _ := extraRootRun(t, "boot-effect-root-c", true, true)
	target, err := filepath.EvalSymlinks(granted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "in.txt")); err == nil {
		t.Fatal("a write through a swapped root landed in the ssh directory")
	}
}

// extraRootFolder is a folder outside the temp tree: a granted root may not
// overlap what every sandboxed command can already write.
func extraRootFolder(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(extraRootBase, "extra-root-*")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(real) })
	return real
}

// extraRootBase is the package directory, read before any test changes into a workspace.
var extraRootBase, _ = os.Getwd()
