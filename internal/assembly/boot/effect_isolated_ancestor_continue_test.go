package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type isolatedForkProvider struct {
	mu      sync.Mutex
	read    string
	refusal string
}

func (*isolatedForkProvider) Name() string { return "isolated-fork" }

func (p *isolatedForkProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := func(id, name string, args any) {
		raw, _ := json.Marshal(args)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}}
	}
	done := func() { ch <- provider.Chunk{Type: provider.ChunkText, Text: "retained fork"} }
	source, fork, copying, reusing := false, false, false, false
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			source = source || strings.Contains(m.Content, "FORK-SOURCE")
			fork = fork || strings.Contains(m.Content, "FORK-WORKER")
			copying = copying || strings.Contains(m.Content, "ANCESTOR-CONTINUE")
			reusing = reusing || strings.Contains(m.Content, "REUSE-COPY")
		}
	}
	switch {
	case fork:
		read, haveRead := callResult(req, "fork-read")
		_, wrote := callResult(req, "fork-write")
		if !haveRead {
			call("fork-read", "read_file", map[string]string{"path": "source.txt"})
		} else if !wrote {
			p.mu.Lock()
			p.read = read
			p.mu.Unlock()
			call("fork-write", "write_file", map[string]string{"path": "fork.txt", "content": "independent fork\n"})
		} else {
			done()
		}
	case source:
		if _, wrote := callResult(req, "source-write"); !wrote {
			call("source-write", "write_file", map[string]string{"path": "source.txt", "content": "retained source\n"})
		} else {
			done()
		}
	default:
		first, created := callResult(req, "source-task")
		second, copied := callResult(req, "fork-task")
		if !created {
			call("source-task", "task", map[string]any{"prompt": "FORK-SOURCE", "isolation": "worktree", "tools": []string{"read_file", "write_file"}, "write_paths": []string{"source.txt", "fork.txt"}})
		} else if !copying {
			done()
		} else if !copied {
			ref := regexp.MustCompile(`sa_[A-Za-z0-9_]+`).FindString(first)
			if ref == "" {
				return nil, fmt.Errorf("source has no transcript: %s", first)
			}
			call("fork-task", "task", map[string]any{"prompt": "FORK-WORKER", "continue_from": ref, "tools": []string{"read_file", "write_file"}, "write_paths": []string{"source.txt", "fork.txt"}})
		} else if _, reused := callResult(req, "reuse-task"); reusing && !reused {
			ref := regexp.MustCompile(`sa_[A-Za-z0-9_]+`).FindString(first)
			call("reuse-task", "task", map[string]any{"prompt": "FORK-WORKER", "continue_from": ref, "tools": []string{"read_file", "write_file"}, "write_paths": []string{"source.txt", "fork.txt"}})
		} else {
			p.mu.Lock()
			p.refusal = second
			p.mu.Unlock()
			done()
		}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectIsolatedAncestorContinuationCopiesSceneWithoutResumingOriginal(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	writeFile(t, home, "config.toml", "[agent]\ncollaboration='shared'\nauto_integrate=false\n[codegraph]\nenabled=false\n")
	writeFile(t, root, "base.txt", "base\n")
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "config", "core.autocrlf", "false")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	p := &isolatedForkProvider{}
	opts := installationEffectOptions(t, home, root, p)
	ctrl, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	bindPresenceSession(t, ctrl)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := ctrl.Run(t.Context(), "create a retained isolated task"); err != nil {
		t.Fatal(err)
	}
	turns := ctrl.CheckpointTurnsByMessageIndex()
	if len(turns) != 1 {
		t.Fatalf("missing branch boundary: %+v", turns)
	}
	var turn int
	for _, value := range turns {
		turn = value
	}
	path, err := ctrl.BranchAt(turn, "continuing descendant")
	if err != nil {
		t.Fatal(err)
	}
	child, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Resume(loaded, path); err != nil {
		t.Fatal(err)
	}
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := child.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	child.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := child.Run(t.Context(), "ANCESTOR-CONTINUE"); err != nil {
		t.Fatal(err)
	}
	catalog, err := child.IsolationResults(t.Context())
	p.mu.Lock()
	seen, outcome := p.read, p.refusal
	p.mu.Unlock()
	if err != nil || len(catalog.Results) != 2 {
		t.Fatalf("isolated fork did not produce an independent retained result: %+v %v; task result=%s", catalog, err, outcome)
	}
	if !strings.Contains(seen, "retained source") {
		t.Fatalf("fork did not inherit the retained scene: %s", seen)
	}
	forks := 0
	for _, r := range catalog.Results {
		if r.Verification != "unknown" {
			t.Fatalf("fork fabricated verification: %+v", r)
		}
		if body, err := os.ReadFile(filepath.Join(r.CandidateRoot, "source.txt")); err != nil || string(body) != "retained source\n" {
			t.Fatalf("retained source changed: %q %v", body, err)
		}
		if body, err := os.ReadFile(filepath.Join(r.CandidateRoot, "fork.txt")); err == nil {
			forks++
			if string(body) != "independent fork\n" {
				t.Fatal("fork content changed")
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if forks != 1 {
		t.Fatalf("fork modified the original scene: %d", forks)
	}
	if err := child.Run(t.Context(), "REUSE-COPY"); err != nil {
		t.Fatal(err)
	}
	after, err := child.IsolationResults(t.Context())
	if err != nil || len(after.Results) != 2 {
		t.Fatalf("ancestor alias created another copy: %+v %v", after, err)
	}
	continued := 0
	for _, row := range after.Results {
		for _, previous := range catalog.Results {
			if row.ID == previous.ID && row.Generation != previous.Generation {
				continued++
			}
		}
	}
	if continued != 1 {
		t.Fatalf("ancestor alias did not resume exactly its independent copy: %d", continued)
	}
	for _, name := range []string{"source.txt", "fork.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("fork wrote source: %s %v", name, err)
		}
	}
}
