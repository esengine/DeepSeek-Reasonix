package tui_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/frontend/serve"
	"reasonix/internal/frontend/tui"
	"reasonix/internal/session/control"
	"reasonix/internal/state/history"
)

func TestMain(m *testing.M) { testenv.RunWithIsolatedUserState(m) }

// scriptedModel answers the first request with prose and a bash call, and the
// one after the call's result with a closing line.
type scriptedModel struct {
	mu    sync.Mutex
	calls int
}

func (p *scriptedModel) Name() string { return "tui-e2e-script" }

func (p *scriptedModel) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 4)
	answered := false
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			answered = true
		}
	}
	if answered {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "all done"}
	} else {
		args, _ := json.Marshal(map[string]string{"command": "touch made-by-tui-e2e && echo tui-e2e-marker"})
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "running it now"}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "b1", Name: "bash", Arguments: string(args)}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// inProcessKernel assembles a real controller behind a hub the way the tui
// command does, and hands back the client the TUI drives it through.
func inProcessKernel(t *testing.T) *tui.Client { return inProcessKernelIn(t, nil) }

// inProcessKernelIn is inProcessKernel with prepare run on the workspace
// before the session opens, so it resolves whatever prepare set up.
func inProcessKernelIn(t *testing.T, prepare func(dir string)) *tui.Client {
	t.Helper()
	home := testenv.TempDir(t)
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME"} {
		t.Setenv(k, home)
	}
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = history.CloseSharedCatalog(ctx)
	})
	dir := testenv.TempDir(t)
	t.Chdir(dir)
	kind := "tui-e2e-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return &scriptedModel{}, nil })
	cfg := `default_model = "test-model"
tool_approval = "ask"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "` + kind + `"
model = "x"
`
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(dir)
	}
	// Unjailed so the run does not depend on the host having an OS sandbox;
	// only the user's own config may say so.
	userConfig := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := (config.Roots{}).ApproveWorkspacePrograms(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte("[sandbox]\nbash = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := serve.NewBroadcaster()
	ctrl, err := boot.Build(context.Background(), boot.Options{Sink: bc})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	hub := serve.NewHub(serve.HubOptions{})
	t.Cleanup(hub.Shutdown)
	if _, err := hub.Adopt(serve.New(ctrl, bc, config.ServeConfig{}), bc); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	return &tui.Client{HTTP: hub.InProcessClient(), Base: "http://reasonix.local/rt/r1"}
}

// One turn through the real kernel, as the TUI sees it: the answer streams, the
// call waits on this screen's approval, runs, and the turn ends settled.
func TestATurnThroughTheInProcessKernel(t *testing.T) {
	c := inProcessKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	updates := c.Subscribe(ctx)

	tr := &tui.Transcript{}
	tr.AddUser("run the marker")
	if err := c.Submit(ctx, "run the marker"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	started := false
	for {
		var u tui.Update
		select {
		case u = <-updates:
		case <-ctx.Done():
			t.Fatalf("turn never finished; transcript %+v", tr.Items)
		}
		if u.Gap {
			t.Fatal("an in-process stream reported a gap")
		}
		tr.Apply(u.Event)
		if u.Event.Kind == "turn_started" {
			started = true
		}
		if open := tr.OpenPrompt(); open != nil && open.Kind == tui.ItemApproval {
			if err := c.Approve(ctx, open.Approval.ID, true, false, false); err != nil {
				t.Fatalf("Approve: %v", err)
			}
			tr.Decide(open.ID, "once")
		}
		if started && u.Event.Kind == "turn_done" {
			break
		}
	}
	if tr.Running || tr.Terminal != tui.TurnCompleted {
		t.Fatalf("running=%v terminal=%v reason=%q", tr.Running, tr.Terminal, tr.EndReason)
	}
	var sawCall, sawApproval, sawClose bool
	for _, it := range tr.Items {
		switch it.Kind {
		case tui.ItemTool:
			sawCall = it.Tool.Name == "bash" && strings.Contains(it.Tool.Output, "tui-e2e-marker") && !it.Running
		case tui.ItemApproval:
			sawApproval = it.Verdict != ""
		case tui.ItemSay:
			sawClose = sawClose || it.Text == "all done"
		}
	}
	if !sawCall || !sawApproval || !sawClose {
		t.Fatalf("call=%v approval=%v close=%v; transcript:\n%s", sawCall, sawApproval, sawClose, dump(tr))
	}
	history, err := c.History(ctx)
	if err != nil || len(history) == 0 {
		t.Fatalf("History = %d, %v", len(history), err)
	}
}

// The footer's workspace@branch comes from the real kernel's session repo, and
// a workspace with no git reports so instead of an empty identity.
func TestWorkspaceGitThroughTheInProcessKernel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if g, err := inProcessKernel(t).WorkspaceGit(ctx); err != nil || g.Repo {
		t.Fatalf("a folder with no git: %+v, %v", g, err)
	}
	c := inProcessKernelIn(t, func(dir string) {
		for _, args := range [][]string{{"init", "-q", "-b", "wip"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base", "--allow-empty"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("git unavailable: %v\n%s", err, out)
			}
		}
	})
	g, err := c.WorkspaceGit(ctx)
	if err != nil || !g.Repo || g.Branch != "wip" || g.Name == "" {
		t.Fatalf("WorkspaceGit = %+v, %v", g, err)
	}
	if g.Untracked != 1 {
		t.Fatalf("reasonix.toml is the one untracked file, got %+v", g)
	}
}

func dump(tr *tui.Transcript) string {
	var b strings.Builder
	for _, it := range tr.Items {
		raw, _ := json.Marshal(it)
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// awaitSubmit sends input and reads the stream until the turn the line starts
// ends (or, for a line that starts none, until its first notice), returning the
// notices seen on the way.
func awaitSubmit(t *testing.T, c *tui.Client, input string, startsTurn bool) []tui.Update {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	updates := c.Subscribe(ctx)
	if err := c.Submit(ctx, input); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var seen []tui.Update
	tr := &tui.Transcript{}
	started := false
	for {
		var u tui.Update
		select {
		case u = <-updates:
		case <-ctx.Done():
			t.Fatalf("%q: stream ended early (turn started=%v)", input, started)
		}
		seen = append(seen, u)
		tr.Apply(u.Event)
		if open := tr.OpenPrompt(); open != nil && open.Kind == tui.ItemApproval {
			if err := c.Approve(ctx, open.Approval.ID, true, false, false); err != nil {
				t.Fatalf("Approve: %v", err)
			}
			tr.Decide(open.ID, "once")
		}
		switch u.Event.Kind {
		case "turn_started":
			started = true
		case "turn_done":
			if started {
				return seen
			}
		case "notice":
			if !startsTurn {
				return seen
			}
		}
	}
}

func userMessages(t *testing.T, c *tui.Client) string {
	t.Helper()
	history, err := c.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, m := range history {
		if m.Role == "user" {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// A slash command no registry answers is sent to the model as an ordinary
// message, and the screen says so (1.x behaviour, #5756).
func TestUnknownSlashCommandIsSentAsAMessageWithANotice(t *testing.T) {
	c := inProcessKernel(t)
	seen := awaitSubmit(t, c, "/definitely-not-a-command now", true)
	var notice string
	for _, u := range seen {
		if u.Event.Kind == "notice" && u.Event.Code == "unknown_command" {
			notice = u.Event.Text
		}
	}
	if !strings.Contains(notice, "/definitely-not-a-command") || !strings.Contains(notice, "sent as a regular message") {
		t.Fatalf("notice = %q", notice)
	}
	if !strings.Contains(userMessages(t, c), "/definitely-not-a-command now") {
		t.Fatal("the line never reached the model as a user message")
	}
}

// A command the registry answers stays out of the conversation, and a slash
// inside multi-line input is prose, not a command.
func TestKnownSlashStaysLocalAndMultilineSlashIsProse(t *testing.T) {
	c := inProcessKernel(t)
	for _, line := range []string{"/context", "/context x", "/new x", "/clear x"} {
		seen := awaitSubmit(t, c, line, false)
		for _, u := range seen {
			if u.Event.Kind == "turn_started" || u.Event.Code == "unknown_command" {
				t.Fatalf("%q was treated as unknown prose: %+v", line, u.Event)
			}
		}
		if got := userMessages(t, c); strings.Contains(got, line) {
			t.Fatalf("a known command reached the conversation: %q", got)
		}
	}
	seen := awaitSubmit(t, c, "see below\n/definitely-not-a-command", true)
	for _, u := range seen {
		if u.Event.Kind == "notice" && u.Event.Code == "unknown_command" {
			t.Fatalf("multi-line input raised the unknown-command notice: %+v", u.Event)
		}
	}
	if !strings.Contains(userMessages(t, c), "see below\n/definitely-not-a-command") {
		t.Fatal("multi-line input did not reach the model")
	}
}
