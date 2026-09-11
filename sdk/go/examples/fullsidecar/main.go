// Command fullsidecar is the reference Reasonix extension sidecar: one small
// program that exercises every Extension Protocol v2 contribution kind —
// input rewriting, tool interception, system-prompt strategy replacement, an
// extension-hosted streaming provider, structured UI surfaces and prompts,
// and a clean bounded shutdown. It is the example third parties copy.
//
// Behavior map:
//
//	input "/fs <text>"        → input.receive replaces the input with
//	                            "<text> [rewritten by fullsidecar]"
//	tool "dangerous_exec"     → tool.before blocks it with a policy reason
//	tool "read"               → tool.before rewrites the arguments (sandbox)
//	system_prompt.build       → the strategy slot owner wraps the prompt
//	session.start             → publishes a status line and a card
//	action "demo"             → asks a form prompt, greets via notification
//	provider plugin/<id>/fake/echo → streams a fixed completion: two text
//	                            chunks, one tool call, usage, done
//
// Environment:
//
//	REASONIX_PLUGIN_NAME   plugin ID, set by the host at launch (provider
//	                       refs must live in the plugin/<id>/ namespace);
//	                       defaults to "fullsidecar" when run standalone
//	FULLSIDECAR_STREAM_INTERVAL_MS
//	                       pacing between provider chunks (default 15)
//
// The two hooks below exist for the host↔SDK conformance suite
// (internal/extension/conformance); they are inert unless set:
//
//	FULLSIDECAR_CRASH_ON_INPUT  exit(3) without answering when an
//	                            input.receive text matches exactly
//	FULLSIDECAR_STALL_ON_INPUT  hold an input.receive answer until the
//	                            intercept context ends when the text matches
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	extension "github.com/esengine/DeepSeek-Reasonix/sdk/go"
)

const (
	rewritePrefix   = "/fs "
	rewriteSuffix   = " [rewritten by fullsidecar]"
	deniedTool      = "dangerous_exec"
	rewrittenTool   = "read"
	fakeModel       = "echo"
	defaultPluginID = "fullsidecar"
)

// plugin is the extension handler. The session context arrives with the
// handshake and is read by later callbacks, so it travels through an atomic.
type plugin struct {
	id      string
	log     *log.Logger
	ui      extension.HostUI
	session atomic.Pointer[extension.SessionContext]
}

func main() {
	logger := log.New(os.Stderr, "fullsidecar: ", log.LstdFlags)
	id := strings.TrimSpace(os.Getenv("REASONIX_PLUGIN_NAME"))
	if id == "" {
		id = defaultPluginID
	}
	p := &plugin{id: id, log: logger}
	provider := &fakeProvider{id: id, interval: streamInterval(), log: logger}
	err := extension.Serve(context.Background(), p, extension.Options{
		Name:    id,
		Version: "1.0.0",
		Interceptors: map[string]extension.InterceptorFunc{
			"input.receive": func(ctx context.Context, _ string, payload json.RawMessage) (*extension.InterceptResult, error) {
				return p.interceptInput(ctx, payload)
			},
			"tool.before": func(ctx context.Context, _ string, payload json.RawMessage) (*extension.InterceptResult, error) {
				return p.interceptTool(ctx, payload)
			},
			"system_prompt.build": func(ctx context.Context, _ string, payload json.RawMessage) (*extension.InterceptResult, error) {
				return p.interceptSystemPrompt(ctx, payload)
			},
		},
		Observer: p.observe,
		Provider: provider,
		UI: extension.UIHandler{
			Action: p.action,
			Submit: p.submit,
		},
		Shutdown: func(context.Context) { logger.Print("shutdown requested; exiting") },
		Logger:   logger,
	})
	if err != nil {
		logger.Printf("serve: %v", err)
		os.Exit(1)
	}
	// Serve returned nil: the host asked for shutdown. Exit 0 so the host
	// reaps the process as an orderly stop.
}

// streamInterval reads FULLSIDECAR_STREAM_INTERVAL_MS with a 15ms default.
func streamInterval() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("FULLSIDECAR_STREAM_INTERVAL_MS")); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 15 * time.Millisecond
}

// Initialize declares everything this extension contributes. The host rejects
// anything the installed manifest did not declare first.
func (p *plugin) Initialize(_ context.Context, params extension.InitializeParams) (*extension.InitializeResult, error) {
	session := params.Session
	p.session.Store(&session)
	p.log.Printf("initialized for session %s (workspace %s)", session.SessionID, session.WorkspaceRoot)
	return &extension.InitializeResult{
		Subscriptions: []string{"input.receive", "tool.before", "system_prompt.build", "session.start"},
		Replaces:      []string{"system_prompt"},
		Providers:     []extension.ProviderDescriptor{fakeDescriptor(p.id)},
		UIActions:     []extension.UIActionDecl{{ActionID: "demo", Label: "Run the fullsidecar demo"}},
		Provides:      append([]extension.CapabilityWire(nil), params.Manifest.Provides...),
	}, nil
}

// Interceptors

// interceptInput rewrites any input that starts with the "/fs " trigger.
func (p *plugin) interceptInput(ctx context.Context, payload json.RawMessage) (*extension.InterceptResult, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return extension.Continue(), nil
	}
	// Conformance hooks (see the package comment); inert when unset.
	if crash := os.Getenv("FULLSIDECAR_CRASH_ON_INPUT"); crash != "" && in.Text == crash {
		p.log.Printf("crash hook triggered by input %q", in.Text)
		os.Exit(3)
	}
	if stall := os.Getenv("FULLSIDECAR_STALL_ON_INPUT"); stall != "" && in.Text == stall {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if !strings.HasPrefix(in.Text, rewritePrefix) {
		return extension.Continue(), nil
	}
	rewritten := strings.TrimPrefix(in.Text, rewritePrefix) + rewriteSuffix
	p.log.Printf("input.receive: rewrote %q → %q", in.Text, rewritten)
	return extension.Replace(map[string]string{"text": rewritten})
}

// interceptTool blocks the denied tool outright and rewrites the arguments of
// the rewritten tool; every other tool continues untouched.
func (p *plugin) interceptTool(_ context.Context, payload json.RawMessage) (*extension.InterceptResult, error) {
	var call struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if err := json.Unmarshal(payload, &call); err != nil {
		return extension.Continue(), nil
	}
	switch call.Name {
	case deniedTool:
		return extension.Block("fullsidecar: tool " + deniedTool + " is denied by the demo policy"), nil
	case rewrittenTool:
		args := map[string]any{}
		if strings.TrimSpace(call.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
				return extension.Continue(), nil
			}
		}
		args["sandbox"] = true
		encoded, err := json.Marshal(args)
		if err != nil {
			return extension.Continue(), nil
		}
		return extension.Replace(map[string]string{"name": call.Name, "arguments": string(encoded)})
	default:
		return extension.Continue(), nil
	}
}

// interceptSystemPrompt owns the system_prompt strategy slot: it wraps the
// base prompt instead of letting the default assembler render it.
func (p *plugin) interceptSystemPrompt(_ context.Context, payload json.RawMessage) (*extension.InterceptResult, error) {
	var in struct {
		Prompt        string `json:"prompt"`
		WorkspaceRoot string `json:"workspaceRoot"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, err
	}
	owned := "You are Reasonix running under the fullsidecar demo strategy.\n\n" +
		"Workspace: " + in.WorkspaceRoot + "\n\nBase prompt:\n" + in.Prompt
	return extension.Replace(map[string]string{"prompt": owned, "workspaceRoot": in.WorkspaceRoot})
}

// Observation and UI

// observe publishes the extension's status line and demo card when the
// session starts.
func (p *plugin) observe(ctx context.Context, event string, _ json.RawMessage) {
	if event != "session.start" {
		return
	}
	session := p.session.Load()
	if session == nil {
		return
	}
	if err := p.ui.PublishStatus(ctx, session.SessionID, session.Generation, "fullsidecar-status", extension.UIStatusPayload{
		Label:    "fullsidecar online",
		Detail:   "intercepts, provider, and UI are live",
		Severity: extension.UISeverityInfo,
	}); err != nil {
		p.log.Printf("publish status: %v", err)
	}
	if err := p.ui.PublishCard(ctx, session.SessionID, session.Generation, "fullsidecar-card", extension.UICardPayload{
		Title:    "fullsidecar",
		Markdown: "Reference extension: try the **demo** action or the `/fs ` input trigger.",
		Fields:   []extension.UIKeyValue{{Key: "plugin", Value: p.id}, {Key: "provider", Value: fakeRef(p.id)}},
		Actions:  []extension.UIActionRef{{ActionID: "demo", Label: "Run demo"}},
	}); err != nil {
		p.log.Printf("publish card: %v", err)
	}
}

// action runs the declared "demo" action: a blocking form prompt, then a
// notification built from the answers. A dismissed prompt is not a failure.
func (p *plugin) action(ctx context.Context, actionID string, _ map[string]string) error {
	if actionID != "demo" {
		return fmt.Errorf("fullsidecar: unknown action %q", actionID)
	}
	session := p.session.Load()
	if session == nil {
		return errors.New("fullsidecar: no session yet")
	}
	values, err := p.ui.RequestForm(ctx, session.SessionID, session.Generation, "fullsidecar-demo-form", extension.UIFormPayload{
		Title:   "fullsidecar demo",
		Message: "Whom should the demo greet?",
		Fields: []extension.UIFormField{
			{Key: "name", Label: "Your name", Kind: extension.UIFieldInput, Required: true},
			{Key: "loud", Label: "Shout the greeting", Kind: extension.UIFieldConfirm},
		},
	})
	if errors.Is(err, extension.ErrUICancelled) {
		return nil
	}
	if err != nil {
		return err
	}
	name, _ := values["name"].(string)
	if strings.TrimSpace(name) == "" {
		name = "world"
	}
	greeting := "Hello, " + name + "!"
	if loud, _ := values["loud"].(bool); loud {
		greeting = strings.ToUpper(greeting)
	}
	return p.ui.PublishNotification(ctx, session.SessionID, session.Generation, "fullsidecar-greeting", extension.UINotificationPayload{
		Title:    greeting,
		Severity: extension.UISeverityInfo,
	})
}

// submit acknowledges published-form submissions with a status update.
func (p *plugin) submit(ctx context.Context, surfaceID string, values map[string]any) error {
	p.log.Printf("form %q submitted: %v", surfaceID, values)
	session := p.session.Load()
	if session == nil {
		return nil
	}
	return p.ui.PublishStatus(ctx, session.SessionID, session.Generation, "fullsidecar-status", extension.UIStatusPayload{
		Label:    "fullsidecar: form " + surfaceID + " submitted",
		Severity: extension.UISeverityInfo,
	})
}

// Fake streaming provider

func fakeRef(pluginID string) string { return "plugin/" + pluginID + "/fake/" + fakeModel }

func fakeDescriptor(pluginID string) extension.ProviderDescriptor {
	return extension.ProviderDescriptor{
		Ref:           fakeRef(pluginID),
		DisplayName:   "fullsidecar fake",
		Model:         fakeModel,
		ContextWindow: 64000,
		Tools:         true,
		Reasoning:     true,
		Efforts:       []string{"low", "high"},
		DefaultEffort: "low",
	}
}

// fakeProvider streams a fixed scripted completion: two text chunks, one tool
// call, final usage, done. Chunks are paced so hosts can exercise mid-stream
// cancel; a cancelled context stops production immediately, and the SDK ends
// the stream interrupted.
type fakeProvider struct {
	id       string
	interval time.Duration
	log      *log.Logger
}

func (p *fakeProvider) Catalog(context.Context) ([]extension.ProviderDescriptor, error) {
	return []extension.ProviderDescriptor{fakeDescriptor(p.id)}, nil
}

func (p *fakeProvider) Stream(ctx context.Context, req extension.StreamRequest) (<-chan extension.StreamChunk, error) {
	if req.ProviderRef != fakeRef(p.id) {
		return nil, fmt.Errorf("fullsidecar: unknown provider ref %q", req.ProviderRef)
	}
	p.log.Printf("stream %s opened for %s (model %s)", req.StreamID, req.ProviderRef, req.Model)
	chunks := make(chan extension.StreamChunk)
	go func() {
		defer close(chunks)
		script := []extension.StreamChunk{
			extension.TextChunk("fake-hello "),
			extension.TextChunk("fake-world"),
			{Type: extension.ChunkToolCall, ToolCall: &extension.ProviderToolCall{
				ID: "call-1", Name: "lookup", Arguments: `{"query":"reasonix"}`,
			}},
			extension.UsageChunk(extension.ProviderUsage{
				PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12,
				CacheHitTokens: 2, CacheMissTokens: 3, ReasoningTokens: 4,
				FinishReason: "stop",
			}),
			extension.DoneChunk(),
		}
		for _, chunk := range script {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.interval):
			}
			select {
			case <-ctx.Done():
				return
			case chunks <- chunk:
			}
		}
	}()
	return chunks, nil
}
