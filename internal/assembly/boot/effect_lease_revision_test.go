package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/builtin"
)

type revisionLeaseProvider struct {
	calls     []provider.ToolCall
	first     chan struct{}
	advance   chan struct{}
	last      chan provider.Message
	finish    chan struct{}
	firstOnce sync.Once
	lastOnce  sync.Once
}

func (*revisionLeaseProvider) Name() string { return "lease-revision" }

func (p *revisionLeaseProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	var results []provider.Message
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleTool {
			results = append(results, msg)
		}
	}
	if len(results) == 1 {
		p.firstOnce.Do(func() { close(p.first) })
		select {
		case <-p.advance:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ch := make(chan provider.Chunk, 2)
	if len(results) < len(p.calls) {
		call := p.calls[len(results)]
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &call}
	} else {
		p.lastOnce.Do(func() { p.last <- results[len(results)-1] })
		select {
		case <-p.finish:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Fixture complete"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func revisionWriter(path string) provider.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path, "content": "fixture\n"})
	return provider.ToolCall{ID: "fixture-write", Name: "write_file", Arguments: string(args)}
}

func revisionLeaseScript(first, second provider.ToolCall) *revisionLeaseProvider {
	first.ID, second.ID = "fixture-first", "fixture-second"
	return &revisionLeaseProvider{calls: []provider.ToolCall{first, second}, first: make(chan struct{}),
		advance: make(chan struct{}), last: make(chan provider.Message, 1), finish: make(chan struct{})}
}

func revisionLeaseWorkspace(t *testing.T) (string, func(provider.Provider, event.Sink, ...builtin.TerminalRunner) *control.Controller) {
	t.Helper()
	isolateConfigHome(t)
	root := robustTempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	kind := "lease-revision-" + filepath.Base(root)
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, root)
	var current provider.Provider
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return current, nil })
	return root, func(p provider.Provider, sink event.Sink, terminals ...builtin.TerminalRunner) *control.Controller {
		current = p
		opts := Options{Sink: sink}
		if len(terminals) != 0 {
			opts.TerminalRunner = terminals[0]
			opts.SandboxBashOverride = "off"
		}
		c, err := Build(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		c.SetToolApprovalMode(control.ToolApprovalYolo)
		t.Cleanup(func() { c.Close() })
		return c
	}
}

func TestEffectControllersRefuseMutualClaimWidening(t *testing.T) {
	for _, whole := range []bool{false, true} {
		t.Run(fmt.Sprintf("whole=%v", whole), func(t *testing.T) {
			root, build := revisionLeaseWorkspace(t)
			secondA, secondB := revisionWriter("b.txt"), revisionWriter("a.txt")
			if whole {
				secondA = provider.ToolCall{Name: "bash", Arguments: `{"command":"echo fixture > opaque-fixture.txt"}`}
				secondB = secondA
			}
			pa, pb := revisionLeaseScript(revisionWriter("a.txt"), secondA), revisionLeaseScript(revisionWriter("b.txt"), secondB)
			frontend := make(chan eventwire.Event, 2)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.ToolResult && e.Tool.ID == "fixture-second" {
					frontend <- eventwire.ToWire(e)
				}
			})
			a, b := build(pa, sink), build(pb, sink)
			a.NameWorkspaceHolder(func() string { return "Fixture A" })
			b.NameWorkspaceHolder(func() string { return "Fixture B" })
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			doneA, doneB := make(chan struct{}), make(chan struct{})
			defer func() { cancel(); close(pa.finish); close(pb.finish); <-doneA; <-doneB }()
			go func() { defer close(doneA); _ = a.Run(ctx, "Write fixture A") }()
			go func() { defer close(doneB); _ = b.Run(ctx, "Write fixture B") }()
			for _, p := range []*revisionLeaseProvider{pa, pb} {
				select {
				case <-p.first:
				case <-ctx.Done():
					t.Fatal("first writes did not finish")
				}
			}
			close(pa.advance)
			close(pb.advance)
			for _, p := range []*revisionLeaseProvider{pa, pb} {
				select {
				case msg := <-p.last:
					if !strings.Contains(msg.Content, "Fixture") || !strings.Contains(msg.Content, ".txt") || !strings.Contains(msg.Content, "workspace.write_conflict") {
						t.Fatalf("model lost holder/path: %s", msg.Content)
					}
				case <-ctx.Done():
					t.Fatal("sessions deadlocked while retaining earlier writes")
				}
			}
			for range 2 {
				select {
				case e := <-frontend:
					data, _ := json.Marshal(e)
					for _, key := range []string{"workspace.write_conflict", "workspaceLease", "holderSessionId", "requestedPaths", "Fixture"} {
						if !strings.Contains(string(data), key) {
							t.Fatalf("frontend lost %s: %s", key, data)
						}
					}
				case <-ctx.Done():
					t.Fatal("frontend missed conflict result")
				}
			}
			for _, path := range []string{"a.txt", "b.txt"} {
				data, err := os.ReadFile(filepath.Join(root, path))
				if err != nil || string(data) != "fixture\n" {
					t.Fatalf("earlier write lost: %q, %v", data, err)
				}
			}
		})
	}
}

func TestEffectWhitespaceWritersCannotOverwriteActiveTurn(t *testing.T) {
	root, build := revisionLeaseWorkspace(t)
	pa := &pathLeaseProvider{path: " leading.txt", written: make(chan struct{}), resume: make(chan struct{})}
	pb := &pathLeaseProvider{path: filepath.Join(root, " leading.txt"), written: make(chan struct{}), resume: make(chan struct{})}
	a, b := build(pa, event.Discard), build(pb, event.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	doneA, doneB := make(chan struct{}), make(chan struct{})
	defer func() { cancel(); close(pa.resume); close(pb.resume); <-doneA; <-doneB }()
	go func() { defer close(doneA); _ = a.Run(ctx, "Write fixture") }()
	go func() {
		defer close(doneB)
		select {
		case <-pa.written:
			_ = b.Run(ctx, "Write fixture")
		case <-ctx.Done():
		}
	}()
	select {
	case <-pa.written:
	case <-ctx.Done():
		t.Fatal("first writer did not finish")
	}
	for !b.WorkspaceLeaseState().Waiting {
		select {
		case <-pb.written:
			t.Fatal("same file acquired through a different spelling")
		case <-ctx.Done():
			t.Fatal("second writer never waited")
		case <-time.After(time.Millisecond):
		}
	}
	if b.WorkspaceLeaseState().Acquired {
		t.Fatal("conflicting writer acquired a lease")
	}
}

func TestEffectSingleSessionWideningAndRestart(t *testing.T) {
	root, build := revisionLeaseWorkspace(t)
	p := revisionLeaseScript(revisionWriter("first.txt"), revisionWriter("second.txt"))
	close(p.advance)
	a := build(p, event.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = a.Run(ctx, "Write two fixtures") }()
	select {
	case <-p.last:
	case <-ctx.Done():
		t.Fatal("single-session widening blocked")
	}
	cancel()
	close(p.finish)
	<-done
	if a.WorkspaceLeaseState().Acquired {
		t.Fatal("ended turn retained claim")
	}
	a.Close()
	p2 := &pathLeaseProvider{path: "first.txt", written: make(chan struct{}), resume: make(chan struct{})}
	b := build(p2, event.Discard)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	done2 := make(chan struct{})
	go func() { defer close(done2); _ = b.Run(ctx2, "Write after restart") }()
	select {
	case <-p2.written:
	case <-ctx2.Done():
		t.Fatal("restarted writer blocked")
	}
	cancel2()
	close(p2.resume)
	<-done2
	for _, name := range []string{"first.txt", "second.txt"} {
		if data, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(data) != "fixture\n" {
			t.Fatalf("restart changed fixture: %q, %v", data, err)
		}
	}
}

func TestEffectAwaitUserReleasesTheWriteClaim(t *testing.T) {
	root, build := revisionLeaseWorkspace(t)
	writeFile(t, root, "go.mod", "module leasefixture\n\ngo 1.21\n")
	writeFile(t, root, "fixture_test.go", "package leasefixture\nimport \"testing\"\nfunc TestFixture(t *testing.T) {}\n")
	p := revisionLeaseScript(provider.ToolCall{Name: "todo_write", Arguments: `{"todos":[{"step_id":"fixture-step","content":"Create fixture and await choice","status":"in_progress"}]}`}, revisionWriter("waiting.txt"))
	p.calls = append(p.calls, provider.ToolCall{ID: "fixture-verify", Name: "bash", Arguments: `{"command":"go test ./..."}`})
	p.calls = append(p.calls, provider.ToolCall{ID: "fixture-await", Name: "await_user", Arguments: `{"step_id":"fixture-step","need":"Select the next fixture task"}`})
	close(p.advance)
	close(p.finish)
	a := build(p, event.Discard, revisionFixtureTerminal{})
	a.EnableInteractiveApproval()
	a.SetToolApprovalMode(control.ToolApprovalYolo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Run(ctx, "Create a fixture, then wait for my next choice"); err != nil {
		t.Fatal(err)
	}
	if a.WorkspaceLeaseState().Acquired {
		t.Fatal("await_user retained the write claim")
	}
	pb := &pathLeaseProvider{path: "other.txt", written: make(chan struct{}), resume: make(chan struct{})}
	b := build(pb, event.Discard)
	done := make(chan struct{})
	go func() { defer close(done); _ = b.Run(ctx, "Write another fixture") }()
	defer func() { cancel(); close(pb.resume); <-done }()
	select {
	case <-pb.written:
	case <-ctx.Done():
		t.Fatal("await_user prevented another session from writing")
	}
}

type revisionFixtureTerminal struct{}

func (revisionFixtureTerminal) RunCommand(_ context.Context, command, _ string, _ time.Duration, _ map[string]string) (string, bool, error) {
	if command != "go test ./..." {
		return "", false, nil
	}
	return "ok  \tleasefixture\t0.001s\n", true, nil
}
