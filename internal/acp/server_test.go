package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

// --- fakes: a Factory wrapping a behavior-driven runner in a real Controller ---

// fakeRunner stands in for an agent.Runner; it emits to the session's sink and
// honors ctx cancellation, but runs no model.
type fakeRunner struct {
	sink     event.Sink
	behavior func(ctx context.Context, sink event.Sink, input string) error
}

func (r *fakeRunner) Run(ctx context.Context, input string) error {
	return r.behavior(ctx, r.sink, input)
}

// fakeFactory builds a real control.Controller around the fake runner, so the
// service exercises the actual controller surface (Run/Cancel/Close) it uses.
type fakeFactory struct {
	behavior func(ctx context.Context, sink event.Sink, input string) error
}

func (f *fakeFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	runner := &fakeRunner{sink: p.Sink, behavior: f.behavior}
	return control.New(control.Options{Runner: runner, Sink: p.Sink}), nil
}

type commandFactory struct {
	commands []command.Command
	skills   []skill.Skill
	seen     chan string
}

func (f *commandFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	runner := &fakeRunner{
		sink: p.Sink,
		behavior: func(_ context.Context, sink event.Sink, input string) error {
			f.seen <- input
			sink.Emit(event.Event{Kind: event.Text, Text: input})
			return nil
		},
	}
	return control.New(control.Options{Runner: runner, Sink: p.Sink, Commands: f.commands, Skills: f.skills}), nil
}

type configurableFactory struct {
	mu         sync.Mutex
	builds     []SessionParams
	dir        string
	withHooks  bool
	hookEvents []hook.Event
	behavior   func(ctx context.Context, sink event.Sink, input string, p SessionParams) error
	managers   []*jobs.Manager
	withCtrl   func(ctx context.Context, sink event.Sink, input string, p SessionParams, ctrl *control.Controller) error
}

func (f *configurableFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	f.mu.Lock()
	f.builds = append(f.builds, SessionParams{
		Cwd:            p.Cwd,
		Model:          p.Model,
		EffortOverride: cloneStringPtr(p.EffortOverride),
		FileOverlay:    p.FileOverlay,
		Terminal:       p.Terminal,
	})
	f.mu.Unlock()
	behavior := f.behavior
	if behavior == nil {
		behavior = func(_ context.Context, sink event.Sink, input string, p SessionParams) error {
			sink.Emit(event.Event{Kind: event.Text, Text: p.Model + ":" + input})
			return nil
		}
	}
	var ctrl *control.Controller
	runner := &fakeRunner{
		sink: p.Sink,
		behavior: func(ctx context.Context, sink event.Sink, input string) error {
			if f.withCtrl != nil {
				return f.withCtrl(ctx, sink, input, p, ctrl)
			}
			return behavior(ctx, sink, input, p)
		},
	}
	opts := control.Options{Runner: runner, Sink: p.Sink, SessionDir: f.dir, OnSessionRecovered: p.OnSessionRecovered}
	if f.withHooks {
		opts.Hooks = f.hookRunner()
	}
	if f.managers != nil {
		jm := jobs.NewManager(event.Discard)
		f.mu.Lock()
		f.managers = append(f.managers, jm)
		f.mu.Unlock()
		opts.Jobs = jm
	}
	ctrl = control.New(opts)
	return ctrl, nil
}

func (f *configurableFactory) SessionDir() string { return f.dir }

type teardownFactory struct {
	dir     string
	grace   time.Duration
	mu      sync.Mutex
	manager *jobs.Manager
}

func (f *teardownFactory) SessionDir() string { return f.dir }

func (f *teardownFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	jm := jobs.NewManager(event.Discard, jobs.WithTeardownGrace(f.grace))
	f.mu.Lock()
	f.manager = jm
	f.mu.Unlock()
	runner := &fakeRunner{
		sink:     p.Sink,
		behavior: func(context.Context, event.Sink, string) error { return nil },
	}
	return control.New(control.Options{
		Runner:     runner,
		Sink:       p.Sink,
		SessionDir: f.dir,
		Jobs:       jm,
	}), nil
}

func (f *teardownFactory) lastManager(t *testing.T) *jobs.Manager {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.manager == nil {
		t.Fatal("session manager was not created")
	}
	return f.manager
}

func (f *configurableFactory) SessionConfigState(_ context.Context, p SessionConfigStateParams) (SessionConfigState, error) {
	model := strings.TrimSpace(p.Model)
	if model == "" {
		model = "fast"
	}
	if model != "fast" && model != "pro" {
		return SessionConfigState{}, os.ErrInvalid
	}
	effort := "auto"
	effortOverride := cloneStringPtr(p.EffortOverride)
	if effortOverride != nil && *effortOverride != "" {
		effort = *effortOverride
	}
	modelOptions := []SessionConfigSelectOption{
		{Value: "fast", Name: "Fast"},
		{Value: "pro", Name: "Pro"},
	}
	effortOptions := []SessionConfigSelectOption{
		{Value: "auto", Name: "Auto"},
		{Value: "high", Name: "High"},
	}
	return SessionConfigState{
		Model:          model,
		EffortOverride: effortOverride,
		Models: &SessionModelState{
			AvailableModels: []ModelInfo{{ModelID: "fast", Name: "Fast"}, {ModelID: "pro", Name: "Pro"}},
			CurrentModelID:  model,
		},
		ConfigOptions: []SessionConfigOption{
			{ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: model, Options: modelOptions},
			{ID: "effort", Name: "Effort", Category: "thought_level", Type: "select", CurrentValue: effort, Options: effortOptions},
		},
	}, nil
}

func (f *configurableFactory) buildAt(t *testing.T, idx int) SessionParams {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.builds) <= idx {
		t.Fatalf("builds = %d, want index %d", len(f.builds), idx)
	}
	return f.builds[idx]
}

func (f *configurableFactory) buildCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.builds)
}

func (f *configurableFactory) managerAt(t *testing.T, idx int) *jobs.Manager {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.managers == nil {
		t.Fatal("factory does not create job managers")
	}
	if len(f.managers) <= idx {
		t.Fatalf("builds = %d, want manager index %d", len(f.builds), idx)
	}
	return f.managers[idx]
}

func (f *configurableFactory) hookRunner() *hook.Runner {
	hooks := []hook.ResolvedHook{
		{HookConfig: hook.HookConfig{Command: "session-start"}, Event: hook.SessionStart},
		{HookConfig: hook.HookConfig{Command: "session-end"}, Event: hook.SessionEnd},
	}
	return hook.NewRunner(hooks, "", func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		var payload hook.Payload
		_ = json.Unmarshal([]byte(in.Stdin), &payload)
		f.mu.Lock()
		f.hookEvents = append(f.hookEvents, payload.Event)
		f.mu.Unlock()
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
}

func (f *configurableFactory) hookEventsSnapshot() []hook.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hook.Event(nil), f.hookEvents...)
}

// --- a minimal JSON-RPC client over the wire, for integration tests ---

type frame struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
	Result json.RawMessage  `json:"result"`
	Error  *rpcError        `json:"error"`
}

type rpcClient struct {
	enc *json.Encoder
	wmu sync.Mutex

	mu     sync.Mutex
	nextID int64
	waits  map[int64]chan frame

	notifs chan frame
	reqs   chan frame
}

func newRPCClient(in io.Writer, out io.Reader) *rpcClient {
	c := &rpcClient{
		enc:    json.NewEncoder(in),
		waits:  make(map[int64]chan frame),
		notifs: make(chan frame, 64),
		reqs:   make(chan frame, 16),
	}
	dec := json.NewDecoder(out)
	go func() {
		for {
			var f frame
			if err := dec.Decode(&f); err != nil {
				return
			}
			switch {
			case f.Method != "" && f.ID != nil:
				c.reqs <- f
			case f.Method != "" && f.ID == nil:
				c.notifs <- f
			case f.Method == "" && f.ID != nil:
				var id int64
				if json.Unmarshal(*f.ID, &id) != nil {
					continue
				}
				c.mu.Lock()
				ch := c.waits[id]
				delete(c.waits, id)
				c.mu.Unlock()
				if ch != nil {
					ch <- f
				}
			}
		}
	}()
	return c
}

func (c *rpcClient) send(v any) {
	c.wmu.Lock()
	_ = c.enc.Encode(v)
	c.wmu.Unlock()
}

func (c *rpcClient) callAsync(method string, params any) chan frame {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan frame, 1)
	c.waits[id] = ch
	c.mu.Unlock()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return ch
}

func (c *rpcClient) call(t *testing.T, method string, params any) frame {
	t.Helper()
	select {
	case f := <-c.callAsync(method, params):
		return f
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: timed out", method)
		return frame{}
	}
}

func (c *rpcClient) notify(method string, params any) {
	c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *rpcClient) reply(id *json.RawMessage, result any) {
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *rpcClient) replyError(id *json.RawMessage, code int, message string) {
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcError{Code: code, Message: message}})
}

func startServer(t *testing.T, factory Factory) (*rpcClient, func()) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		_ = Serve(context.Background(), inR, outW, factory, AgentInfo{Name: "reasonix-test", Version: "0"})
		close(done)
	}()
	client := newRPCClient(inW, outR)
	return client, func() {
		_ = inW.Close()
		<-done
		_ = outW.Close()
	}
}

// drainPrompt collects session/update notifications until the prompt's response
// arrives, then sweeps any notifications still buffered.
func drainPrompt(t *testing.T, c *rpcClient, promptCh chan frame) ([]frame, frame) {
	t.Helper()
	var notifs []frame
	var resp frame
	for {
		select {
		case f := <-c.notifs:
			notifs = append(notifs, f)
		case resp = <-promptCh:
			for {
				select {
				case f := <-c.notifs:
					notifs = append(notifs, f)
				default:
					return notifs, resp
				}
			}
		case <-time.After(2 * time.Second):
			t.Fatal("session/prompt: timed out")
		}
	}
}

func updateKind(t *testing.T, f frame) string {
	t.Helper()
	var p struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
		} `json:"update"`
	}
	if err := json.Unmarshal(f.Params, &p); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	return p.Update.SessionUpdate
}

func configOptionValueFromUpdate(t *testing.T, f frame, id string) (string, bool) {
	t.Helper()
	var p struct {
		Update struct {
			SessionUpdate string                `json:"sessionUpdate"`
			ConfigOptions []SessionConfigOption `json:"configOptions"`
		} `json:"update"`
	}
	if err := json.Unmarshal(f.Params, &p); err != nil {
		t.Fatalf("decode config update: %v", err)
	}
	if p.Update.SessionUpdate != "config_option_update" {
		return "", false
	}
	opt, ok := findConfigOption(p.Update.ConfigOptions, id)
	if !ok {
		return "", false
	}
	return opt.CurrentValue, true
}

func messageChunkText(t *testing.T, f frame) (string, bool) {
	t.Helper()
	var p struct {
		Update struct {
			SessionUpdate string       `json:"sessionUpdate"`
			Content       ContentBlock `json:"content"`
		} `json:"update"`
	}
	if err := json.Unmarshal(f.Params, &p); err != nil {
		t.Fatalf("decode message update: %v", err)
	}
	if p.Update.SessionUpdate != "agent_message_chunk" || p.Update.Content.Type != "text" {
		return "", false
	}
	return p.Update.Content.Text, true
}

// --- tests ---

func TestServeLifecycle(t *testing.T) {
	factory := &fakeFactory{behavior: func(_ context.Context, sink event.Sink, input string) error {
		sink.Emit(event.Event{Kind: event.Text, Text: "hi " + input})
		sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "ls", Args: `{}`}})
		sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c1", Name: "ls", Output: "file.go"}})
		return nil
	}}
	client, stop := startServer(t, factory)
	defer stop()

	initResp := client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	var ir InitializeResult
	if err := json.Unmarshal(initResp.Result, &ir); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	if ir.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %d, want %d", ir.ProtocolVersion, ProtocolVersion)
	}
	if !ir.AgentCapabilities.PromptCapabilities.EmbeddedContext {
		t.Errorf("embeddedContext should be advertised")
	}
	if ir.AgentCapabilities.SessionCapabilities.List == nil ||
		ir.AgentCapabilities.SessionCapabilities.Resume == nil ||
		ir.AgentCapabilities.SessionCapabilities.Close == nil ||
		ir.AgentCapabilities.SessionCapabilities.Delete == nil {
		t.Errorf("sessionCapabilities = %+v, want list/resume/close/delete", ir.AgentCapabilities.SessionCapabilities)
	}
	if ir.AgentCapabilities.PromptCapabilities.Image {
		t.Errorf("image must not be advertised")
	}
	if len(ir.AuthMethods) != 1 || ir.AuthMethods[0].ID != "reasonix-setup" || ir.AuthMethods[0].Type != "terminal" {
		t.Fatalf("authMethods = %+v, want terminal reasonix setup", ir.AuthMethods)
	}
	if len(ir.AuthMethods[0].Args) != 1 || ir.AuthMethods[0].Args[0] != "setup" {
		t.Fatalf("auth args = %+v, want [setup]", ir.AuthMethods[0].Args)
	}

	authResp := client.call(t, "authenticate", AuthenticateParams{MethodID: "reasonix-setup"})
	if authResp.Error != nil {
		t.Fatalf("authenticate errored: %+v", authResp.Error)
	}
	badAuthResp := client.call(t, "authenticate", AuthenticateParams{MethodID: "missing"})
	if badAuthResp.Error == nil || badAuthResp.Error.Code != ErrInvalidParams {
		t.Fatalf("bad authenticate = %+v, want invalid params", badAuthResp.Error)
	}

	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil || nr.SessionID == "" {
		t.Fatalf("session/new result: %v (%q)", err, nr.SessionID)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "there"}},
	})
	notifs, resp := drainPrompt(t, client, promptCh)

	kinds := map[string]bool{}
	for _, n := range notifs {
		kinds[updateKind(t, n)] = true
	}
	for _, want := range []string{"agent_message_chunk", "tool_call", "tool_call_update"} {
		if !kinds[want] {
			t.Errorf("missing %s update; saw %v", want, kinds)
		}
	}
	var pr SessionPromptResult
	if err := json.Unmarshal(resp.Result, &pr); err != nil {
		t.Fatalf("prompt result: %v", err)
	}
	if pr.StopReason != StopEndTurn {
		t.Errorf("stopReason = %q, want %q", pr.StopReason, StopEndTurn)
	}
}

func TestServeAdvertisesAndExpandsCustomCommands(t *testing.T) {
	factory := &commandFactory{
		seen: make(chan string, 1),
		skills: []skill.Skill{{
			Name: "writing-plans", Plugin: "superpowers", Description: "Write a plan", Body: "Plan $ARGUMENTS",
		}},
		commands: []command.Command{
			{
				Name:        "review",
				Description: "Review the target",
				ArgHint:     "path",
				Body:        "Review $1",
			},
			{
				Name:   "plan",
				Body:   "Plan $ARGUMENTS",
				Hidden: true,
			},
		},
	}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil || nr.SessionID == "" {
		t.Fatalf("session/new result: %v (%q)", err, nr.SessionID)
	}

	var advertised bool
	var hiddenAdvertised bool
	var pluginSkillAdvertised bool
	select {
	case n := <-client.notifs:
		var p struct {
			Update struct {
				SessionUpdate     string             `json:"sessionUpdate"`
				AvailableCommands []AvailableCommand `json:"availableCommands"`
			} `json:"update"`
		}
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Fatalf("available commands update: %v", err)
		}
		for _, cmd := range p.Update.AvailableCommands {
			if cmd.Name == "superpowers:writing-plans" {
				pluginSkillAdvertised = true
			}
			if cmd.Name == "writing-plans" {
				hiddenAdvertised = true
			}
			if cmd.Name == "plan" {
				hiddenAdvertised = true
			}
			if p.Update.SessionUpdate == "available_commands_update" &&
				cmd.Name == "review" &&
				cmd.Description == "Review the target" &&
				cmd.Input != nil &&
				cmd.Input.Hint == "path" {
				advertised = true
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for available_commands_update")
	}
	if !advertised {
		t.Fatal("review command was not advertised")
	}
	if hiddenAdvertised {
		t.Fatal("hidden compatibility command was advertised")
	}
	if !pluginSkillAdvertised {
		t.Fatal("qualified plugin skill was not advertised")
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "/review src/main.go"}},
	})
	_, resp := drainPrompt(t, client, promptCh)
	if resp.Error != nil {
		t.Fatalf("prompt errored: %+v", resp.Error)
	}
	select {
	case got := <-factory.seen:
		if got != "Review src/main.go" {
			t.Fatalf("runner input = %q, want expanded command", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not receive prompt")
	}
}

func TestServeSessionConfigSwitchesModelAndEffort(t *testing.T) {
	factory := &configurableFactory{}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}
	if nr.Models == nil || nr.Models.CurrentModelID != "fast" {
		t.Fatalf("models = %+v, want current fast", nr.Models)
	}
	modelOpt, ok := findConfigOption(nr.ConfigOptions, "model")
	if !ok || modelOpt.CurrentValue != "fast" {
		t.Fatalf("model config = %+v, want current fast", modelOpt)
	}
	if got := factory.buildAt(t, 0).Model; got != "fast" {
		t.Fatalf("initial build model = %q, want fast", got)
	}

	setModelResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "model",
		Value:     "pro",
	})
	var modelSet SetSessionConfigOptionResult
	if err := json.Unmarshal(setModelResp.Result, &modelSet); err != nil {
		t.Fatalf("set model result: %v", err)
	}
	modelOpt, _ = findConfigOption(modelSet.ConfigOptions, "model")
	if modelOpt.CurrentValue != "pro" {
		t.Fatalf("model after set_config_option = %q, want pro", modelOpt.CurrentValue)
	}
	if got := factory.buildAt(t, 1).Model; got != "pro" {
		t.Fatalf("second build model = %q, want pro", got)
	}

	setEffortResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "effort",
		Value:     "high",
	})
	var effortSet SetSessionConfigOptionResult
	if err := json.Unmarshal(setEffortResp.Result, &effortSet); err != nil {
		t.Fatalf("set effort result: %v", err)
	}
	effortOpt, _ := findConfigOption(effortSet.ConfigOptions, "effort")
	if effortOpt.CurrentValue != "high" {
		t.Fatalf("effort after set_config_option = %q, want high", effortOpt.CurrentValue)
	}
	effortBuild := factory.buildAt(t, 2)
	if effortBuild.Model != "pro" || effortBuild.EffortOverride == nil || *effortBuild.EffortOverride != "high" {
		t.Fatalf("effort build = model:%q effort:%v, want pro/high", effortBuild.Model, effortBuild.EffortOverride)
	}

	setLegacyResp := client.call(t, "session/set_model", SetSessionModelParams{SessionID: nr.SessionID, ModelID: "fast"})
	if setLegacyResp.Error != nil {
		t.Fatalf("session/set_model errored: %+v", setLegacyResp.Error)
	}
	if got := factory.buildAt(t, 3).Model; got != "fast" {
		t.Fatalf("legacy set_model build model = %q, want fast", got)
	}
}

func TestServeSessionConfigQueuesDuringActivePrompt(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	factory := &configurableFactory{
		behavior: func(ctx context.Context, sink event.Sink, input string, p SessionParams) error {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			sink.Emit(event.Event{Kind: event.Text, Text: p.Model + ":" + input})
			return nil
		},
	}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	first := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "first"}},
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("prompt never started")
	}

	setResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "model",
		Value:     "pro",
	})
	if setResp.Error != nil {
		t.Fatalf("set_config_option while running errored: %+v", setResp.Error)
	}
	var set SetSessionConfigOptionResult
	if err := json.Unmarshal(setResp.Result, &set); err != nil {
		t.Fatalf("set model result: %v", err)
	}
	modelOpt, _ := findConfigOption(set.ConfigOptions, "model")
	if modelOpt.CurrentValue != "pro" {
		t.Fatalf("queued model option = %q, want pro", modelOpt.CurrentValue)
	}
	if got := factory.buildCount(); got != 1 {
		t.Fatalf("build count while prompt is active = %d, want only initial build", got)
	}

	close(release)
	_, resp := drainPrompt(t, client, first)
	if resp.Error != nil {
		t.Fatalf("first prompt errored: %+v", resp.Error)
	}
	if got := factory.buildAt(t, 1).Model; got != "pro" {
		t.Fatalf("queued rebuild model = %q, want pro", got)
	}
}

func TestServeSessionConfigRejectsBackgroundJobsWhileIdle(t *testing.T) {
	dir := t.TempDir()
	factory := &configurableFactory{dir: dir, managers: []*jobs.Manager{}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	jm := factory.managerAt(t, 0)
	release := make(chan struct{})
	var releaseOnce sync.Once
	started := make(chan struct{})
	sessionPath := transcriptPath(dir, nr.SessionID)
	jm.StartForSession(agent.BranchID(sessionPath), "bash", "server", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		select {
		case <-release:
			return "", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	defer func() {
		releaseOnce.Do(func() { close(release) })
		jm.Close()
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never started")
	}

	setResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "model",
		Value:     "pro",
	})
	if setResp.Error == nil || !strings.Contains(setResp.Error.Message, "stop background jobs") {
		t.Fatalf("set_config_option with background job error = %+v, want stop background jobs RPC error", setResp.Error)
	}
	legacyResp := client.call(t, "session/set_model", SetSessionModelParams{SessionID: nr.SessionID, ModelID: "pro"})
	if legacyResp.Error == nil || !strings.Contains(legacyResp.Error.Message, "stop background jobs") {
		t.Fatalf("set_model with background job error = %+v, want stop background jobs RPC error", legacyResp.Error)
	}
	if got := factory.buildCount(); got != 1 {
		t.Fatalf("build count after rejected switch = %d, want 1", got)
	}
	if running := jm.RunningForSession(agent.BranchID(sessionPath)); len(running) != 1 {
		t.Fatalf("running jobs after rejected switch = %+v, want original job still running", running)
	}

	releaseOnce.Do(func() { close(release) })
	_ = jm.WaitForSession(context.Background(), agent.BranchID(sessionPath), nil, 5)
	if running := jm.RunningForSession(agent.BranchID(sessionPath)); len(running) != 0 {
		t.Fatalf("running jobs after release = %+v, want none before retry", running)
	}

	retryResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "model",
		Value:     "pro",
	})
	if retryResp.Error != nil {
		t.Fatalf("retry set_config_option after jobs stopped errored: %+v", retryResp.Error)
	}
	var retry SetSessionConfigOptionResult
	if err := json.Unmarshal(retryResp.Result, &retry); err != nil {
		t.Fatalf("retry set_config_option result: %v", err)
	}
	modelOpt, _ := findConfigOption(retry.ConfigOptions, "model")
	if modelOpt.CurrentValue != "pro" {
		t.Fatalf("retry model currentValue = %q, want pro", modelOpt.CurrentValue)
	}
	if got := factory.buildCount(); got != 2 {
		t.Fatalf("build count after retry switch = %d, want rebuild", got)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "after-switch"}},
	})
	notifs, resp := drainPrompt(t, client, promptCh)
	if resp.Error != nil {
		t.Fatalf("prompt after retry switch errored: %+v", resp.Error)
	}
	var usedNewModel bool
	for _, n := range notifs {
		if text, ok := messageChunkText(t, n); ok && strings.Contains(text, "pro:after-switch") {
			usedNewModel = true
			break
		}
	}
	if !usedNewModel {
		t.Fatalf("prompt after retry did not use new model; notifications=%+v", notifs)
	}
}

func TestServeQueuedSessionConfigDiscardedWhenPromptLeavesBackgroundJob(t *testing.T) {
	dir := t.TempDir()
	releaseJob := make(chan struct{})
	releaseTurn := make(chan struct{})
	startedJob := make(chan struct{})
	startedTurn := make(chan struct{})
	var jobOnce sync.Once
	factory := &configurableFactory{dir: dir, managers: []*jobs.Manager{}}
	factory.behavior = func(ctx context.Context, sink event.Sink, input string, p SessionParams) error {
		if input == "first" {
			close(startedTurn)
			jm := factory.managerAt(t, 0)
			jobOnce.Do(func() {
				jm.StartForSession(jobs.SessionFromContext(ctx), "bash", "server", func(ctx context.Context, _ io.Writer) (string, error) {
					close(startedJob)
					select {
					case <-releaseJob:
						return "", nil
					case <-ctx.Done():
						return "", ctx.Err()
					}
				})
			})
			select {
			case <-startedJob:
			case <-time.After(2 * time.Second):
				t.Fatal("background job never started")
			}
			select {
			case <-releaseTurn:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		sink.Emit(event.Event{Kind: event.Text, Text: p.Model + ":" + input})
		return nil
	}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	first := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "first"}},
	})
	select {
	case <-startedTurn:
	case <-time.After(2 * time.Second):
		t.Fatal("prompt never started")
	}
	setResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "model",
		Value:     "pro",
	})
	if setResp.Error != nil {
		t.Fatalf("set_config_option while prompt is running errored: %+v", setResp.Error)
	}

	close(releaseTurn)
	notifs, resp := drainPrompt(t, client, first)
	if resp.Error != nil {
		t.Fatalf("first prompt errored: %+v", resp.Error)
	}
	warningIndex := -1
	for i, n := range notifs {
		if text, ok := messageChunkText(t, n); ok && strings.Contains(text, "stop background jobs") {
			warningIndex = i
			break
		}
	}
	if warningIndex < 0 {
		t.Fatalf("queued switch updates = %d, want warning mentioning background jobs", len(notifs))
	}
	var sawOldConfig bool
	for _, n := range notifs[warningIndex+1:] {
		if value, ok := configOptionValueFromUpdate(t, n, "model"); ok && value == "fast" {
			sawOldConfig = true
			break
		}
	}
	if !sawOldConfig {
		t.Fatalf("queued switch notifications after warning did not include model currentValue=fast: %+v", notifs[warningIndex+1:])
	}

	close(releaseJob)
	jm := factory.managerAt(t, 0)
	_ = jm.WaitForSession(context.Background(), agent.BranchID(transcriptPath(dir, nr.SessionID)), nil, 5)
	second := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "second"}},
	})
	_, resp = drainPrompt(t, client, second)
	if resp.Error != nil {
		t.Fatalf("second prompt errored: %+v", resp.Error)
	}
	if got := factory.buildCount(); got != 1 {
		t.Fatalf("build count after discarded queued switch = %d, want 1", got)
	}
}

func TestServeSessionConfigRejectsPendingAsk(t *testing.T) {
	factory := &configurableFactory{
		withCtrl: func(ctx context.Context, _ event.Sink, _ string, _ SessionParams, ctrl *control.Controller) error {
			_, err := ctrl.Ask(ctx, []event.AskQuestion{{
				ID:      "choice",
				Prompt:  "Pick one",
				Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
			}})
			return err
		},
	}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "ask"}},
	})
	var req frame
	select {
	case req = <-client.reqs:
	case <-time.After(2 * time.Second):
		t.Fatal("ask request was not sent to client")
	}

	setResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "model",
		Value:     "pro",
	})
	if setResp.Error == nil || !strings.Contains(setResp.Error.Message, "pending") {
		t.Fatalf("set_config_option with pending ask error = %+v, want pending interaction RPC error", setResp.Error)
	}
	if got := factory.buildCount(); got != 1 {
		t.Fatalf("build count while ask is pending = %d, want 1", got)
	}

	client.reply(req.ID, PermissionRequestResult{
		Outcome: PermissionOutcome{Outcome: "selected", OptionID: "choice:1"},
	})
	_, resp := drainPrompt(t, client, promptCh)
	if resp.Error != nil {
		t.Fatalf("prompt errored: %+v", resp.Error)
	}
	if got := factory.buildCount(); got != 1 {
		t.Fatalf("build count after answered ask = %d, want no queued rebuild", got)
	}
}

func TestServeSessionConfigRebuildPreservesLifecycleHooks(t *testing.T) {
	factory := &configurableFactory{withHooks: true}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "one"}},
	})
	_, resp := drainPrompt(t, client, promptCh)
	if resp.Error != nil {
		t.Fatalf("first prompt errored: %+v", resp.Error)
	}
	if got := factory.hookEventsSnapshot(); len(got) != 1 || got[0] != hook.SessionStart {
		t.Fatalf("hook events after first prompt = %v, want [SessionStart]", got)
	}

	setResp := client.call(t, "session/set_config_option", SetSessionConfigOptionParams{
		SessionID: nr.SessionID,
		ConfigID:  "model",
		Value:     "pro",
	})
	if setResp.Error != nil {
		t.Fatalf("set_config_option errored: %+v", setResp.Error)
	}
	if got := factory.hookEventsSnapshot(); len(got) != 1 || got[0] != hook.SessionStart {
		t.Fatalf("hook events after config rebuild = %v, want no lifecycle hook", got)
	}

	promptCh = client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "two"}},
	})
	_, resp = drainPrompt(t, client, promptCh)
	if resp.Error != nil {
		t.Fatalf("second prompt errored: %+v", resp.Error)
	}
	if got := factory.hookEventsSnapshot(); len(got) != 1 || got[0] != hook.SessionStart {
		t.Fatalf("hook events after second prompt = %v, want no duplicate SessionStart", got)
	}

	closeResp := client.call(t, "session/close", SessionCloseParams{SessionID: nr.SessionID})
	if closeResp.Error != nil {
		t.Fatalf("session/close errored: %+v", closeResp.Error)
	}
	if got := factory.hookEventsSnapshot(); len(got) != 2 || got[0] != hook.SessionStart || got[1] != hook.SessionEnd {
		t.Fatalf("hook events after close = %v, want [SessionStart SessionEnd]", got)
	}
}

func TestServeSessionLoadFallsBackFromStaleSavedModel(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sessionID := "stale-model"
	path := transcriptPath(dir, sessionID)
	saved := agent.NewSession("")
	saved.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := saved.Save(path); err != nil {
		t.Fatal(err)
	}
	effort := "high"
	if err := saveACPMeta(path, acpSessionMeta{
		SessionID:      sessionID,
		Cwd:            cwd,
		Model:          "missing/model",
		EffortOverride: &effort,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	factory := &configurableFactory{dir: dir}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	loadResp := client.call(t, "session/load", SessionLoadParams{SessionID: sessionID, Cwd: cwd})
	if loadResp.Error != nil {
		t.Fatalf("session/load with stale saved model errored: %+v", loadResp.Error)
	}
	if got := factory.buildAt(t, 0).Model; got != "fast" {
		t.Fatalf("fallback build model = %q, want fast", got)
	}
	meta, ok, err := loadACPMeta(path)
	if err != nil || !ok {
		t.Fatalf("load rewritten meta = %v, ok=%v", err, ok)
	}
	if meta.Model != "fast" {
		t.Fatalf("rewritten meta model = %q, want fast", meta.Model)
	}
}

func TestServeSessionLoadRejectsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sessionID := "pending-load"
	path := transcriptPath(dir, sessionID)
	saved := agent.NewSession("")
	saved.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := saved.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := saveACPMeta(path, acpSessionMeta{
		SessionID: sessionID,
		Cwd:       cwd,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}

	factory := &configurableFactory{dir: dir}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	loadResp := client.call(t, "session/load", SessionLoadParams{SessionID: sessionID, Cwd: cwd})
	if loadResp.Error == nil || !strings.Contains(loadResp.Error.Message, "unknown session") {
		t.Fatalf("session/load cleanup-pending error = %+v, want unknown session", loadResp.Error)
	}
	factory.mu.Lock()
	builds := append([]SessionParams(nil), factory.builds...)
	factory.mu.Unlock()
	if len(builds) != 0 {
		t.Fatalf("cleanup-pending load should not build a controller, got builds %+v", builds)
	}
}

func TestServeCancel(t *testing.T) {
	started := make(chan struct{})
	factory := &fakeFactory{behavior: func(ctx context.Context, _ event.Sink, _ string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	json.Unmarshal(newResp.Result, &nr)

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "loop"}},
	})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("prompt never started")
	}
	client.notify("session/cancel", SessionCancelParams{SessionID: nr.SessionID})

	select {
	case resp := <-promptCh:
		var pr SessionPromptResult
		json.Unmarshal(resp.Result, &pr)
		if pr.StopReason != StopCancelled {
			t.Errorf("stopReason = %q, want cancelled", pr.StopReason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not end the prompt")
	}
}

func TestServePromptErrorIsNotReportedAsCancelled(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error {
		return errors.New("provider failed")
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	json.Unmarshal(newResp.Result, &nr)

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "fail"}},
	})
	_, resp := drainPrompt(t, client, promptCh)
	var pr SessionPromptResult
	if err := json.Unmarshal(resp.Result, &pr); err != nil {
		t.Fatalf("prompt result: %v", err)
	}
	if pr.StopReason != StopError {
		t.Errorf("stopReason = %q, want error", pr.StopReason)
	}
}

func TestServeRejectsConcurrentPromptForSameSession(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	factory := &fakeFactory{behavior: func(_ context.Context, _ event.Sink, _ string) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	json.Unmarshal(newResp.Result, &nr)

	first := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "first"}},
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first prompt never started")
	}

	second := client.call(t, "session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "second"}},
	})
	if second.Error == nil {
		t.Fatal("second concurrent prompt should return an error")
	}
	if second.Error.Code != ErrInvalidRequest || !strings.Contains(second.Error.Message, "active prompt") {
		t.Fatalf("second prompt error = %+v, want active-prompt invalid request", second.Error)
	}

	close(release)
	select {
	case resp := <-first:
		if resp.Error != nil {
			t.Fatalf("first prompt errored: %+v", resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first prompt did not finish")
	}
}

func TestServeSessionClose(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error { return nil }}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	json.Unmarshal(newResp.Result, &nr)

	closeResp := client.call(t, "session/close", SessionCloseParams{SessionID: nr.SessionID})
	if closeResp.Error != nil {
		t.Fatalf("session/close errored: %+v", closeResp.Error)
	}

	promptResp := client.call(t, "session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "after close"}},
	})
	if promptResp.Error == nil || !strings.Contains(promptResp.Error.Message, "unknown session") {
		t.Fatalf("prompt after close error = %+v, want unknown session", promptResp.Error)
	}
}

func TestSessionDeleteWithStuckJobReturnsAfterSingleGrace(t *testing.T) {
	dir := t.TempDir()
	grace := time.Second
	maxElapsed := grace + 750*time.Millisecond
	factory := &teardownFactory{dir: dir, grace: grace}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil || nr.SessionID == "" {
		t.Fatalf("session/new: %v (%q)", err, nr.SessionID)
	}
	path := transcriptPath(dir, nr.SessionID)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	releaseJob := startNonCooperativeACPJob(t, factory.lastManager(t), path)
	defer releaseJob()

	start := time.Now()
	resp := client.call(t, "session/delete", SessionDeleteParams{SessionID: nr.SessionID})
	elapsed := time.Since(start)
	if resp.Error != nil {
		t.Fatalf("session/delete errored: %+v", resp.Error)
	}
	if elapsed > maxElapsed {
		t.Fatalf("session/delete took %s, want one teardown grace plus scheduling slack", elapsed)
	}
	if !agent.IsCleanupPending(path) {
		t.Fatalf("stuck ACP delete should mark cleanup pending")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stuck ACP transcript should remain until delayed cleanup: %v", err)
	}
	releaseJob()
	deadline := time.Now().Add(2 * time.Second)
	for agent.IsCleanupPending(path) {
		if time.Now().After(deadline) {
			t.Fatalf("cleanup-pending marker was not cleared after stuck job release")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeRejectsPathLikeSessionID(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error { return nil }}
	client, stop := startServer(t, factory)
	defer stop()

	resp := client.call(t, "session/delete", SessionDeleteParams{SessionID: "../outside"})
	if resp.Error == nil {
		t.Fatal("session/delete with path-like sessionId should fail")
	}
	if resp.Error.Code != ErrInvalidParams || !strings.Contains(resp.Error.Message, "invalid sessionId") {
		t.Fatalf("session/delete error = %+v, want invalid sessionId", resp.Error)
	}
}

func TestListACPMetasSkipsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	visibleID := "visible"
	pendingID := "pending"
	for _, id := range []string{visibleID, pendingID} {
		path := transcriptPath(dir, id)
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := saveACPMeta(path, acpSessionMeta{
			SessionID: id,
			Cwd:       t.TempDir(),
			Title:     id,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := agent.MarkCleanupPending(transcriptPath(dir, pendingID), "delete"); err != nil {
		t.Fatal(err)
	}

	metas, err := listACPMetas(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].SessionID != visibleID {
		t.Fatalf("listACPMetas = %+v, want only %q", metas, visibleID)
	}
}

func TestDeleteSessionFilesDeletesOwnedSubagents(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeACPSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))
	jobsDir := jobs.ArtifactDir(sessionPath)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, "bash-1.log"), []byte("output"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := deleteSessionFiles(sessionPath); err != nil {
		t.Fatalf("deleteSessionFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", ref+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("subagent jsonl should be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", ref+".meta.json")); !os.IsNotExist(err) {
		t.Fatalf("subagent meta should be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(jobsDir); !os.IsNotExist(err) {
		t.Fatalf("jobs sidecar should be deleted, stat err = %v", err)
	}
}

func TestReconcileCleanupPendingDeletesACPMeta(t *testing.T) {
	dir := t.TempDir()
	sessionPath := transcriptPath(dir, "pending-acp")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveACPMeta(sessionPath, acpSessionMeta{Cwd: t.TempDir(), Model: "test-model"}); err != nil {
		t.Fatal(err)
	}
	jobsDir := jobs.ArtifactDir(sessionPath)
	if err := os.MkdirAll(jobsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsDir, "bash-1.log"), []byte("output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkCleanupPending(sessionPath, "delete"); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileCleanupPending(dir); err != nil {
		t.Fatalf("ReconcileCleanupPending: %v", err)
	}
	for _, path := range []string{sessionPath, acpMetaPath(sessionPath), jobsDir, agent.CleanupPendingPath(sessionPath)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after reconciliation (err=%v)", path, err)
		}
	}
}

func writeACPSubagentArtifact(t *testing.T, dir, ref, parentSession string) {
	t.Helper()
	subagentDir := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, ref+".jsonl"), []byte(`{"role":"user","content":"sub"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(agent.SubagentMeta{
		Ref:           ref,
		Status:        agent.SubagentCompleted,
		Kind:          "task",
		Name:          "task",
		ParentSession: parentSession,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, ref+".meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func startNonCooperativeACPJob(t *testing.T, jm *jobs.Manager, sessionPath string) func() {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	jm.StartForSession(agent.BranchID(sessionPath), "bash", "stuck job", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		<-release
		return "", ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never started")
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		close(release)
	}
}

func TestServeUnknownMethod(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error { return nil }}
	client, stop := startServer(t, factory)
	defer stop()

	resp := client.call(t, "does/not/exist", nil)
	if resp.Error == nil {
		t.Fatal("expected an error response")
	}
	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrMethodNotFound)
	}
}
