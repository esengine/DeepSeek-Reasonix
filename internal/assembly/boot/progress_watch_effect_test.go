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

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
)

// stallScriptProvider plays one tool call per agent round from a script, then
// answers. Requests without a tool surface (titles, triage) get plain text and
// do not advance the script.
type stallScriptProvider struct {
	mu     sync.Mutex
	rounds int
	script func(round int) *provider.ToolCall
	prompt int
	reqs   []provider.Request
}

func (p *stallScriptProvider) Name() string { return "boot-progress-watch" }

func (p *stallScriptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 4)
	defer close(ch)
	if len(req.Tools) == 0 {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "title"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		return ch, nil
	}
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.rounds++
	call := p.script(p.rounds)
	p.mu.Unlock()
	if p.prompt > 0 {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: p.prompt, CompletionTokens: 10, TotalTokens: p.prompt + 10}}
	}
	if call != nil {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: call}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	return ch, nil
}

func (p *stallScriptProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.reqs...)
}

type watchSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *watchSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func (s *watchSink) reports() []event.ProgressWatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.ProgressWatch
	for _, e := range s.events {
		if e.Kind == event.ProgressWatchEvent && e.ProgressWatch != nil {
			out = append(out, *e.ProgressWatch)
		}
	}
	return out
}

func toolCall(id, name string, args map[string]string) *provider.ToolCall {
	raw, _ := json.Marshal(args)
	return &provider.ToolCall{ID: id, Name: name, Arguments: string(raw)}
}

// echoLoop is the #11153 shape: every call succeeds, every argument differs,
// and nothing on disk changes or is read.
func echoLoop(n int) func(int) *provider.ToolCall {
	return func(round int) *provider.ToolCall {
		if round > n {
			return nil
		}
		return toolCall(fmt.Sprintf("echo-%d", round), "bash", map[string]string{"command": fmt.Sprintf("echo step %d of the plan", round)})
	}
}

// researchLoop reads something new every round, by file tool, search and shell.
func researchLoop(dir string, n int) func(int) *provider.ToolCall {
	return func(round int) *provider.ToolCall {
		if round > n {
			return nil
		}
		id := fmt.Sprintf("research-%d", round)
		switch round % 3 {
		case 0:
			return toolCall(id, "read_file", map[string]string{"path": filepath.Join(dir, "notes", fmt.Sprintf("n%02d.md", round))})
		case 1:
			return toolCall(id, "grep", map[string]string{"pattern": fmt.Sprintf("marker%02d", round), "path": "notes"})
		default:
			return toolCall(id, "bash", map[string]string{"command": fmt.Sprintf("cat notes/n%02d.md", round)})
		}
	}
}

// editTestLoop alternates a change with a check of it, the ordinary fix loop.
func editTestLoop(dir string, n int) func(int) *provider.ToolCall {
	return func(round int) *provider.ToolCall {
		if round > n {
			return nil
		}
		id := fmt.Sprintf("edit-%d", round)
		if round%2 == 1 {
			return toolCall(id, "write_file", map[string]string{"path": filepath.Join(dir, "calc.txt"), "content": fmt.Sprintf("version %d\n", round)})
		}
		return toolCall(id, "bash", map[string]string{"command": "go test ./..."})
	}
}

type watchRun struct {
	err     error
	reports []event.ProgressWatch
	reqs    []provider.Request
	ctrl    *control.Controller
}

// runWatch builds the real stack with userConfig in the user file and
// projectExtra in reasonix.toml, lets arm touch the controller, and runs once.
func runWatch(t *testing.T, script func(dir string) func(int) *provider.ToolCall, userConfig, projectExtra string, prompt int, arm func(*control.Controller)) watchRun {
	return runWatchWindow(t, script, userConfig, projectExtra, "", prompt, arm)
}

func runWatchWindow(t *testing.T, script func(dir string) func(int) *provider.ToolCall, userConfig, projectExtra, window string, prompt int, arm func(*control.Controller)) watchRun {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 40 {
		writeFile(t, filepath.Join(dir, "notes"), fmt.Sprintf("n%02d.md", i), fmt.Sprintf("note %d marker%02d\n", i, i))
	}
	if userConfig != "" {
		path := config.UserConfigPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(userConfig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	kind := "boot-progress-" + strings.NewReplacer("/", "-", " ", "-").Replace(strings.ToLower(t.Name()))
	rec := &stallScriptProvider{script: script(dir), prompt: prompt}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
`+projectExtra+`
[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`+window+`
`)
	approveWorkspace(t, dir)
	sink := &watchSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if arm != nil {
		arm(ctrl)
	}
	runErr := ctrl.Run(context.Background(), "work through the task")
	return watchRun{err: runErr, reports: sink.reports(), reqs: rec.requests(), ctrl: ctrl}
}

func stalledReports(reports []event.ProgressWatch) []event.ProgressWatch {
	var out []event.ProgressWatch
	for _, r := range reports {
		if r.Stalled {
			out = append(out, r)
		}
	}
	return out
}

// No host-authored byte rides a request: every user message the provider saw
// is the one the run began with.
func assertNothingSaidToTheModel(t *testing.T, reqs []provider.Request) {
	t.Helper()
	if len(reqs) == 0 {
		t.Fatal("no agent request reached the provider")
	}
	users := func(r provider.Request) int {
		n := 0
		for _, m := range r.Messages {
			if m.Role == provider.RoleUser {
				n++
			}
		}
		return n
	}
	first := users(reqs[0])
	for i, r := range reqs {
		if got := users(r); got != first {
			t.Fatalf("request %d carries %d user messages, the run began with %d: the host said something to the model", i, got, first)
		}
		for _, m := range r.Messages {
			if strings.Contains(m.Content, "observable progress") || strings.Contains(m.Content, "progress_watch") {
				t.Fatalf("request %d carries the watch's wording: %q", i, m.Content)
			}
		}
	}
}

// Default configuration, #11153 shape: the notice appears once the loop passes
// the default twenty rounds, the run is never paused, and the model is told
// nothing.
func TestEffectProgressWatchNoticesTheVaryingEchoLoopByDefault(t *testing.T) {
	const rounds = 24
	run := runWatch(t, func(string) func(int) *provider.ToolCall { return echoLoop(rounds) }, "", "", 0, nil)
	if run.err != nil {
		t.Fatalf("the default configuration must never pause a run: %v", run.err)
	}
	if len(run.reqs) != rounds+1 {
		t.Fatalf("the loop was cut short: %d agent requests, want %d", len(run.reqs), rounds+1)
	}
	stalled := stalledReports(run.reports)
	if len(stalled) == 0 {
		t.Fatalf("no stall was reported for %d varying echo rounds: %+v", rounds, run.reports)
	}
	first := stalled[0]
	if first.Cause != event.ProgressWatchCauseRounds || first.RoundLimit != config.DefaultProgressWatchRounds || first.IdleRounds != config.DefaultProgressWatchRounds || first.Pausing {
		t.Fatalf("first stall report = %+v, want rounds cause at the default limit, not pausing", first)
	}
	assertNothingSaidToTheModel(t, run.reqs)
}

// The user's switch, saved through the controller the settings screen calls,
// persists to the user file and pauses the running kind of loop resumably.
func TestEffectProgressWatchPausesOnceTheUserTurnsItOn(t *testing.T) {
	run := runWatch(t, func(string) func(int) *provider.ToolCall { return echoLoop(30) }, "", "", 0, func(c *control.Controller) {
		if err := c.SaveProgressWatchSettings(control.ProgressWatchSettings{Pause: true, Rounds: 4, TokenMultiple: 8}); err != nil {
			t.Fatalf("save: %v", err)
		}
	})
	info, ok := agent.InspectRunPause(run.err)
	if !ok || info.Kind != agent.PauseKindNoProgress || info.Limit != 4 {
		t.Fatalf("run error = %v (pause %+v, %v), want a no_progress pause at 4 rounds", run.err, info, ok)
	}
	if len(run.reqs) != 4 {
		t.Fatalf("%d agent requests, want 4: the pause lands at the round boundary with no further sampling", len(run.reqs))
	}
	stalled := stalledReports(run.reports)
	if len(stalled) != 1 || !stalled[0].Pausing || stalled[0].IdleRounds != 4 {
		t.Fatalf("stall reports = %+v, want exactly one pausing report at 4 idle rounds", stalled)
	}
	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil || !strings.Contains(string(body), "[progress_watch]") || !strings.Contains(string(body), "pause = true") {
		t.Fatalf("the setting did not reach the user file: %v\n%s", err, body)
	}
	if got := run.ctrl.ProgressWatchSettings(); !got.Pause || got.Rounds != 4 {
		t.Fatalf("settings read back = %+v", got)
	}
	assertNothingSaidToTheModel(t, run.reqs)
}

// Saving the pause switch through the settings screen preserves the retry
// budget stored beside it: the screen does not edit that knob, so it must not
// clear it.
func TestEffectProgressWatchSavePreservesRetryBudget(t *testing.T) {
	runWatch(t, func(string) func(int) *provider.ToolCall { return echoLoop(1) },
		"[progress_watch]\nperseveration_retries = 2\n", "", 0, func(c *control.Controller) {
			if err := c.SaveProgressWatchSettings(control.ProgressWatchSettings{Pause: true, Rounds: 4, TokenMultiple: 8}); err != nil {
				t.Fatalf("save: %v", err)
			}
		})
	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if !strings.Contains(string(body), "perseveration_retries = 2") {
		t.Fatalf("saving progress watch settings dropped the retry budget:\n%s", body)
	}
}

const pausingUserConfig = `
[progress_watch]
pause = true
rounds = 4
token_multiple = 1000
`

// Reading something new every round — a file, a search, a shell cat — is
// progress, even with the most eager configuration.
func TestEffectProgressWatchLeavesResearchAlone(t *testing.T) {
	run := runWatch(t, func(dir string) func(int) *provider.ToolCall { return researchLoop(dir, 18) }, pausingUserConfig, "", 0, nil)
	if run.err != nil {
		t.Fatalf("research was paused: %v", run.err)
	}
	if stalled := stalledReports(run.reports); len(stalled) > 0 {
		t.Fatalf("research was reported as stalled: %+v", stalled)
	}
	if len(run.reqs) != 19 {
		t.Fatalf("%d agent requests, want 19", len(run.reqs))
	}
}

// A change followed by a check of it never reads as stalled, even while the
// check keeps failing the same way.
func TestEffectProgressWatchLeavesAnEditTestLoopAlone(t *testing.T) {
	run := runWatch(t, func(dir string) func(int) *provider.ToolCall { return editTestLoop(dir, 12) }, pausingUserConfig, "", 0, nil)
	if _, paused := agent.InspectRunPause(run.err); paused {
		t.Fatalf("the edit/test loop was paused: %v", run.err)
	}
	if stalled := stalledReports(run.reports); len(stalled) > 0 {
		t.Fatalf("the edit/test loop was reported as stalled: %+v", stalled)
	}
}

// A repository's reasonix.toml cannot switch the pause on or move the limits:
// the section is the user's.
func TestEffectProgressWatchProjectConfigCannotPause(t *testing.T) {
	run := runWatch(t, func(string) func(int) *provider.ToolCall { return echoLoop(8) }, "", pausingUserConfig, 0, nil)
	if run.err != nil {
		t.Fatalf("a project file paused the user's run: %v", run.err)
	}
	if stalled := stalledReports(run.reports); len(stalled) > 0 {
		t.Fatalf("a project file moved the round limit: %+v", stalled)
	}
}

// The model-independent backstop counts input spent since the run last did
// anything observable, so a loop that reads and changes nothing trips it on
// spend alone, before the round limit.
func TestEffectProgressWatchTokenBackstop(t *testing.T) {
	run := runWatchWindow(t, func(string) func(int) *provider.ToolCall { return echoLoop(6) }, `
[progress_watch]
rounds = 1000
token_multiple = 1
`, "", "context_window = 100000", 30000, nil)
	if run.err != nil {
		t.Fatalf("the backstop paused a run with pause off: %v", run.err)
	}
	stalled := stalledReports(run.reports)
	if len(stalled) == 0 || stalled[0].Cause != event.ProgressWatchCauseTokens || stalled[0].TokenLimit != 100000 || stalled[0].PromptTokens < 100000 {
		t.Fatalf("stall reports = %+v, want a tokens report at the 100000-token limit", stalled)
	}
	assertNothingSaidToTheModel(t, run.reqs)
}

// A long run that does something observable every round is never stalled,
// however far its total input passes window x N: each effect restarts the
// backstop's count along with the round count.
func TestEffectProgressWatchLeavesALongProductiveRunAlone(t *testing.T) {
	const rounds = 15
	run := runWatchWindow(t, func(dir string) func(int) *provider.ToolCall { return researchLoop(dir, rounds) }, `
[progress_watch]
pause = true
rounds = 4
token_multiple = 1
`, "", "context_window = 100000", 30000, nil)
	if run.err != nil {
		t.Fatalf("a productive run was paused: %v", run.err)
	}
	if stalled := stalledReports(run.reports); len(stalled) > 0 {
		t.Fatalf("a productive run was reported as stalled: %+v", stalled)
	}
	if len(run.reqs) != rounds+1 || (rounds+1)*30000 < 4*100000 {
		t.Fatalf("%d agent requests, want %d spending well past 1x the window", len(run.reqs), rounds+1)
	}
}
