package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/schedrun"
	"reasonix/internal/state/schedule"
)

const (
	asCLIEnv        = "RX_TEST_AS_CLI"
	asSupervisorEnv = "RX_TEST_AS_SUPERVISOR"
)

// scheduleTestHelperProcess makes the test binary stand in for the two programs
// a scheduled run involves: the reasonix command the supervisor starts, and a
// supervisor process a test can kill.
func scheduleTestHelperProcess() (int, bool) {
	if id := os.Getenv(asSupervisorEnv); id != "" {
		_ = os.Unsetenv(asSupervisorEnv)
		store, err := schedule.Open(config.RootsForHome("").ScheduleDir())
		if err != nil {
			return 20, true
		}
		sup := &schedrun.Supervisor{Store: store, Policy: schedule.DefaultPolicy()}
		_, _ = sup.Run(context.Background(), id)
		return 0, true
	}
	if os.Getenv(asCLIEnv) == "1" {
		return Run(os.Args[1:], "test"), true
	}
	return 0, false
}

type fakeTurn struct {
	text       string
	callName   string
	callArgs   string
	prompt, cl int
	noUsage    bool
}

// fakeModel answers the n-th request with turns[n], repeating the last turn, and
// keeps every request body for the test to read back.
type fakeModel struct {
	srv     *httptest.Server
	mu      sync.Mutex
	turns   []fakeTurn
	bodies  []map[string]any
	block   chan struct{}
	started chan struct{}
	once    sync.Once
}

func newFakeModel(t *testing.T, turns ...fakeTurn) *fakeModel {
	t.Helper()
	m := &fakeModel{turns: turns, started: make(chan struct{})}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(func() {
		if m.block != nil {
			close(m.block)
		}
		m.srv.Close()
	})
	return m
}

func (m *fakeModel) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	m.mu.Lock()
	m.bodies = append(m.bodies, body)
	n := min(len(m.bodies)-1, len(m.turns)-1)
	turn := m.turns[n]
	block := m.block
	m.mu.Unlock()
	m.once.Do(func() { close(m.started) })
	if block != nil {
		select {
		case <-block:
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	usage := fmt.Sprintf(`"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}`, turn.prompt, turn.cl, turn.prompt+turn.cl)
	if turn.noUsage {
		usage = `"x":0`
	}
	if turn.callName != "" {
		call, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
			"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call_%d", n), "type": "function",
				"function": map[string]any{"name": turn.callName, "arguments": turn.callArgs}}},
		}}}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", call)
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],`+usage+"}\n\ndata: [DONE]\n\n")
		return
	}
	text, _ := json.Marshal(turn.text)
	_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":`+string(text)+`},"finish_reason":"stop"}],`+usage+"}\n\ndata: [DONE]\n\n")
}

func (m *fakeModel) requests() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.bodies)
}

func (m *fakeModel) hang() { m.mu.Lock(); m.block = make(chan struct{}); m.mu.Unlock() }

func requestTools(req map[string]any) []string {
	var names []string
	tools, _ := req["tools"].([]any)
	for _, t := range tools {
		fn, _ := t.(map[string]any)["function"].(map[string]any)
		if n, _ := fn["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names
}

func requestRole(req map[string]any, role string) string {
	var out []string
	msgs, _ := req["messages"].([]any)
	for _, raw := range msgs {
		msg, _ := raw.(map[string]any)
		if msg["role"] != role {
			continue
		}
		switch c := msg["content"].(type) {
		case string:
			out = append(out, c)
		default:
			b, _ := json.Marshal(c)
			out = append(out, string(b))
		}
	}
	return strings.Join(out, "\n")
}

type execWorld struct {
	t     *testing.T
	model *fakeModel
	store *schedule.Store
	clk   *testClock
	ws    string
	sc    schedule.Schedule
	run   schedule.Run
	pol   schedule.Policy
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// newExecWorld isolates the user's configuration, points a provider at a fake
// model, and leaves one claimed run of a confirmed schedule in the store.
func newExecWorld(t *testing.T, schedTable string, prompt string, model schedule.Model, turns ...fakeTurn) *execWorld {
	t.Helper()
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("SCHED_FAKE_KEY", "k")
	fm := newFakeModel(t, turns...)
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf(`default_model = "fake"

[[providers]]
name = "fake"
kind = "openai"
base_url = %q
model = "fake-model"
api_key_env = "SCHED_FAKE_KEY"

%s
`, fm.srv.URL, schedTable), 0o644)
	user, err := schedule.LoadOverrides(config.RootsForHome("").UserConfigLoadPath())
	if err != nil {
		t.Fatal(err)
	}
	pol, _, err := schedule.Resolve(user, schedule.Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	clk := &testClock{t: time.Now().UTC()}
	base, err := schedule.Open(config.RootsForHome("").ScheduleDir())
	if err != nil {
		t.Fatal(err)
	}
	store := base.WithClock(clk.Now)
	ws := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(ws, "notes.txt"), "the TODO list is empty\n", 0o644)
	writeTestFile(t, filepath.Join(ws, "reasonix.toml"), "default_model = \"evil/none\"\n", 0o644)
	sc, err := store.Create(t.Context(), pol, schedule.CreateRequest{
		Trigger: schedule.Trigger{Kind: schedule.TriggerEvery, EverySec: 3600}, Target: schedule.Target{Workspace: ws},
		Prompt: prompt, Model: model, ConfirmedBy: schedule.ConfirmedByHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(3601 * time.Second)
	slot, _ := sc.Trigger.LatestSlot(clk.Now())
	run, err := store.Claim(t.Context(), pol, sc.ID, slot)
	if err != nil {
		t.Fatal(err)
	}
	return &execWorld{t: t, model: fm, store: store, clk: clk, ws: ws, sc: sc, run: run, pol: pol}
}

var fakeModelRef = schedule.Model{Provider: "fake", Model: "fake-model"}

// child starts `reasonix schedule exec` as a separate process the way the
// supervisor does, but leaves the stdin pipe to the test.
func (w *execWorld) child() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "schedule", "exec", w.run.TriggerID)
	cmd.Env = append(os.Environ(), asCLIEnv+"=1")
	cmd.Dir = os.TempDir()
	return cmd
}

func (w *execWorld) supervisor() *schedrun.Supervisor {
	return &schedrun.Supervisor{Store: w.store, Policy: w.pol, WallGrace: 5 * time.Second,
		Command: func(string) (*exec.Cmd, error) { return w.child(), nil }}
}

func (w *execWorld) runRecord() schedule.Run {
	w.t.Helper()
	m, _, err := w.store.Snapshot(w.t.Context())
	if err != nil {
		w.t.Fatal(err)
	}
	for _, r := range m.Runs {
		if r.TriggerID == w.run.TriggerID {
			return r
		}
	}
	w.t.Fatal("run vanished")
	return schedule.Run{}
}
