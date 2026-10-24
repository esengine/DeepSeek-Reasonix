package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessiontemp"
	"reasonix/internal/tools/jobs"
)

type ownedJobProbe struct {
	started chan *jobs.Manager
	release chan struct{}
	temp    chan bool
}

func (*ownedJobProbe) Name() string            { return "start_owned_job" }
func (*ownedJobProbe) Description() string     { return "Start a run-owned test job." }
func (*ownedJobProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*ownedJobProbe) ReadOnly() bool          { return true }
func (p *ownedJobProbe) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	m, ok := jobs.FromContext(ctx)
	if !ok {
		return "", errors.New("isolated execution has no owned manager")
	}
	temp := sessiontemp.FromContext(ctx)
	_, err := m.StartOwnedForSession(jobs.SessionFromContext(ctx), "task", "owned", func(context.Context, io.Writer) (string, error) {
		p.started <- m
		<-p.release
		p.temp <- temp != nil && !temp.Sealed()
		return "done", nil
	})
	return "started", err
}

func TestIsolatedTaskOwnsJobsAndJoinsBeforeReturning(t *testing.T) {
	probe := &ownedJobProbe{started: make(chan *jobs.Manager, 1), release: make(chan struct{}), temp: make(chan bool, 1)}
	reg := tool.NewRegistry()
	reg.Add(probe)
	prov := &mockProvider{name: "owned-jobs", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "owned", Name: probe.Name(), Arguments: `{}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskToolWithOptions(TaskToolOptions{Provider: prov, ParentRegistry: reg, MaxSteps: 8, SysPrompt: "worker"}).
		WithTranscripts(NewSubagentStore(t.TempDir()), t.TempDir(), "model", "high")
	task.environmentID = "iso_fixture"
	caller := jobs.NewManager(event.Discard)
	t.Cleanup(caller.Close)
	ctx := jobs.WithManager(testTaskContext(), caller)
	returned := make(chan error, 1)
	go func() {
		_, err := task.Execute(ctx, json.RawMessage(`{"prompt":"start one owned job"}`))
		returned <- err
	}()
	manager := <-probe.started
	if manager == caller {
		t.Fatal("isolated child borrowed its caller manager")
	}
	select {
	case err := <-returned:
		t.Fatalf("task released environment while job was active: %v", err)
	default:
	}
	close(probe.release)
	if !<-probe.temp {
		t.Error("execution-private temporary resources were released before its job exited")
	}
	if err := <-returned; err != nil {
		t.Fatal(err)
	}
	_, err := manager.StartOwnedForSession("new", "task", "late", func(context.Context, io.Writer) (string, error) {
		t.Error("completed run admitted a new job")
		return "", nil
	})
	if !errors.Is(err, jobs.ErrAdmissionClosed) {
		t.Fatalf("completed execution admission: %v", err)
	}
}

func TestOwnedJobsNeverInheritIntoReadOnlyOrSiblingExecution(t *testing.T) {
	writer := &profileExecution{task: &TaskTool{environmentID: "iso_parent"}}
	first, closeFirst := writer.ownExecutionJobs(t.Context(), event.Discard)
	var err error
	defer closeFirst(&err)
	second, closeSecond := writer.ownExecutionJobs(first, event.Discard)
	defer closeSecond(&err)
	left := first.Value(executionJobsKey{}).(*jobs.Manager)
	right := second.Value(executionJobsKey{}).(*jobs.Manager)
	if left == nil || right == nil || left == right {
		t.Fatal("distinct executions shared their job owner")
	}
	reader := &profileExecution{task: writer.task, spec: ProfileExecSpec{Grant: CapabilityGrant{ReadOnly: true}}}
	readCtx, closeRead := reader.ownExecutionJobs(first, event.Discard)
	defer closeRead(&err)
	if readCtx.Value(executionJobsKey{}).(*jobs.Manager) != nil {
		t.Fatal("read-only execution inherited writer jobs")
	}
}

func TestIsolatedJobToolsRespectReadOnlyAndExplicitCeilings(t *testing.T) {
	reg := tool.NewRegistry()
	for _, name := range []string{"bash", "wait", "bash_output", "kill_shell", "read_file"} {
		reg.Add(subagentRegistryTool{name: name, readOnly: name == "read_file", schema: `{"type":"object","properties":{"run_in_background":{"type":"boolean"}}}`})
	}
	task := &TaskTool{parentReg: reg, environmentID: "iso_fixture"}
	writer := task.buildSubReg(nil, 1)
	for _, name := range []string{"wait", "bash_output", "kill_shell"} {
		if _, ok := writer.Get(name); !ok {
			t.Errorf("owned writer omitted %s", name)
		}
	}
	bash, _ := writer.Get("bash")
	if !strings.Contains(string(bash.Schema()), "run_in_background") {
		t.Fatal("owned writer still advertises foreground-only bash")
	}
	bounded := task.buildSubReg([]string{"read_file"}, 1)
	if _, ok := bounded.Get("wait"); ok {
		t.Fatal("owned job surface exceeded the caller's whitelist")
	}
	spec := ProfileExecSpec{Grant: CapabilityGrant{ReadOnly: true, CallTools: []string{"read_file", "wait", "bash_output", "kill_shell"}}}
	reader, err := task.subRegistryFor(&spec, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wait", "bash_output", "kill_shell"} {
		if _, ok := reader.Get(name); ok {
			t.Errorf("read-only child exposed %s", name)
		}
	}
}
