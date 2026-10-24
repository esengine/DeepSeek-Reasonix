package delegation

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func TestSubagentRunningPublicationRequiresDurableTranscript(t *testing.T) {
	for _, previous := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "continued"}[previous], func(t *testing.T) {
			store := NewSubagentStore(t.TempDir())
			spec := testSubagentSpec(t, "review")
			run, err := store.PrepareFresh(spec)
			if err != nil {
				t.Fatal(err)
			}
			defer run.Release()
			if previous {
				if err := store.SaveCompleted(run); err != nil {
					t.Fatal(err)
				}
				run.Release()
				run, err = store.PrepareContinue(run.Ref, spec)
				if err != nil {
					t.Fatal(err)
				}
				defer run.Release()
				if err := os.Remove(store.sessionPath(run.Ref) + ".lock"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(store.sessionPath(run.Ref)+".lock", 0700); err != nil {
				t.Fatal(err)
			}
			if err := store.MarkRunning(run); err == nil {
				t.Fatal("running published without durable transcript")
			}
			if store.liveSession(run.Ref) != nil {
				t.Fatal("failed admission exposed a live worker")
			}
			meta, err := store.LoadMeta(run.Ref)
			if previous {
				if err != nil || meta.Status != sessionstore.SubagentCompleted {
					t.Fatalf("prior terminal marker changed: %+v %v", meta, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fresh failed admission published metadata: %+v %v", meta, err)
			}
		})
	}
}

type admissionProbeProvider struct {
	provider.Provider
	probe func(provider.Request)
}

func (p admissionProbeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.probe(req)
	return p.Provider.Stream(ctx, req)
}

func TestSubagentAdmittedPromptIsDurableBeforeProviderAndRefusesFailedSave(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable", true: "failed"}[fail], func(t *testing.T) {
			store := NewSubagentStore(t.TempDir())
			run, err := store.PrepareFresh(testSubagentSpec(t, "review"))
			if err != nil {
				t.Fatal(err)
			}
			defer run.Release()
			if err := store.MarkRunning(run); err != nil {
				t.Fatal(err)
			}
			calls := 0
			prov := admissionProbeProvider{Provider: &mockProvider{name: "admission", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}}, probe: func(req provider.Request) {
				calls++
				saved, err := sessionstore.LoadSession(store.sessionPath(run.Ref))
				if err != nil || !slices.ContainsFunc(saved.Snapshot(), func(m provider.Message) bool {
					return m.Role == provider.RoleUser && m.RawContent == "original delegated task"
				}) {
					t.Fatalf("provider started before original task was durable: %v", err)
				}
			}}
			opts := (&TaskTool{transcripts: store}).steerable(agent.Options{}, run.Session)
			if fail {
				if err := os.Remove(store.sessionPath(run.Ref) + ".lock"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(store.sessionPath(run.Ref)+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			}
			_, err = agent.RunSubAgentWithSession(t.Context(), prov, tool.NewRegistry(), run.Session, "original delegated task", opts, event.Discard)
			if fail {
				if err == nil || calls != 0 {
					t.Fatalf("failed admission executed: calls=%d err=%v", calls, err)
				}
			} else if err != nil || calls == 0 {
				t.Fatalf("durable admission failed: calls=%d err=%v", calls, err)
			}
		})
	}
}
