package delegation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func TestMutationExecutionUsesActualChildSessionAndLiveGeneration(t *testing.T) {
	for _, mode := range []string{"valid", "foreign_agent", "read_only", "planning", "cancelled", "replaced_generation", "released", "starting", "required_writer_missing"} {
		t.Run(mode, func(t *testing.T) {
			store, spec := viewFixture(t)
			run := startedRun(t, store, spec)
			t.Cleanup(run.Release)
			session := run.Session
			if mode == "required_writer_missing" {
				session.RequireWriteAuthority()
			}
			if mode == "foreign_agent" {
				session = sessionstore.NewSession("unused environment executor")
			}
			source := agent.New(nil, tool.NewRegistry(), session, agent.Options{ReadOnlyExecution: mode == "read_only"}, event.Discard)
			if mode == "planning" {
				source.SetPlanMode(true)
			}
			if mode != "starting" {
				store.attachSteerer(run.Session, source)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			scoped := (&TaskTool{transcripts: store}).withMutationExecution(ctx, run)
			validate, ok := MutationExecutionValidator(scoped)
			if !ok {
				t.Fatal("actual run has no execution validator")
			}
			switch mode {
			case "cancelled":
				cancel()
			case "replaced_generation":
				store.setLive(run.Ref, run.Session)
				store.attachSteerer(run.Session, source)
			case "released":
				run.Release()
			}
			err := validate(t.Context())
			switch mode {
			case "valid":
				if err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled source retained authority: %v", err)
				}
			default:
				var refusal tool.Refusal
				if !errors.As(err, &refusal) || refusal.Code != "isolation.stale_execution" {
					t.Fatalf("stale source retained authority: %v", err)
				}
			}
		})
	}
}

type mutationExecutionProbe struct {
	validate func(context.Context) error
	valid    bool
}

func (*mutationExecutionProbe) Name() string            { return "inspect_execution_owner" }
func (*mutationExecutionProbe) Description() string     { return "Inspect the host execution owner." }
func (*mutationExecutionProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*mutationExecutionProbe) ReadOnly() bool          { return true }
func (p *mutationExecutionProbe) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	validate, ok := MutationExecutionValidator(ctx)
	p.validate = validate
	p.valid = ok && validate(ctx) == nil
	if !p.valid {
		return "", errors.New("actual child execution validator is unavailable")
	}
	return "execution owner checked", nil
}

func TestTaskExecutionBindsChildAuthorityAndPreservesProviderPrefix(t *testing.T) {
	probe := &mutationExecutionProbe{}
	reg := tool.NewRegistry()
	reg.Add(probe)
	prov := &mockProvider{name: "execution-owner", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "owner", Name: probe.Name(), Arguments: `{}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskToolWithOptions(TaskToolOptions{Provider: prov, ParentRegistry: reg, MaxSteps: 8, SysPrompt: "worker"}).
		WithTranscripts(NewSubagentStore(t.TempDir()), t.TempDir(), "model", "high")
	if _, err := task.Execute(testTaskContext(), json.RawMessage(`{"prompt":"inspect execution identity"}`)); err != nil {
		t.Fatal(err)
	}
	if !probe.valid || probe.validate == nil {
		t.Fatal("production child did not receive its live authority")
	}
	if err := probe.validate(t.Context()); err == nil {
		t.Fatal("completed child retained integration authority")
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider calls: %d", len(prov.requests))
	}
	first, _ := json.Marshal(prov.requests[0].Tools)
	second, _ := json.Marshal(prov.requests[1].Tools)
	if !bytes.Equal(first, second) || prov.requests[0].Messages[0].Content != prov.requests[1].Messages[0].Content {
		t.Fatal("dynamic execution ownership changed the provider prefix")
	}
}

func TestBackgroundContextCarriesAndRetiresActualParentAuthority(t *testing.T) {
	store, spec := viewFixture(t)
	run := startedRun(t, store, spec)
	t.Cleanup(run.Release)
	source := agent.New(nil, tool.NewRegistry(), run.Session, agent.Options{}, event.Discard)
	store.attachSteerer(run.Session, source)
	parent := (&TaskTool{transcripts: store}).withMutationExecution(t.Context(), run)
	job := backgroundProfileContext(t.Context(), parent)
	validate, ok := MutationExecutionValidator(job)
	if !ok || validate(t.Context()) != nil {
		t.Fatal("background preparation lost its actual parent source")
	}
	run.Release()
	if err := validate(t.Context()); err == nil {
		t.Fatal("background source outlived its parent execution")
	}
}
