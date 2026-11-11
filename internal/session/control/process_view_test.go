package control

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/process"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/tools/jobs"
)

type liveSubagents struct {
	SubagentReader
	runs []delegation.RunInfo
}

func (l liveSubagents) LiveRuns(parent, workspace string) ([]delegation.RunInfo, error) {
	return l.runs, nil
}

func TestProcessesListsWhatIsRunningAcrossOwners(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	t.Cleanup(m.Close)
	job := m.Start("bash", "sleep 60", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	dir := testenv.TempDir(t)
	c := New(Options{Sink: event.Discard, SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"), Jobs: m, Subagents: liveSubagents{runs: []delegation.RunInfo{
		{Ref: "sa_1", Name: "audit", State: delegation.RunRunning, ParentTurn: "turn-3"},
		{Ref: "sa_2", Name: "late", State: delegation.RunCompleted},
	}}})

	got, err := c.Processes()
	if err != nil {
		t.Fatalf("Processes: %v", err)
	}
	byID := map[string]process.Process{}
	for _, p := range got {
		byID[p.ID] = p
	}
	if len(got) != 2 {
		t.Fatalf("got %d processes %+v, want the running job and the running sub-agent only", len(got), got)
	}
	if p := byID[process.ID(process.KindJob, job.ID)]; p.Kind != process.KindJob || p.State != process.Running || p.Name != "sleep 60" {
		t.Fatalf("job process = %+v", p)
	}
	if p := byID["subagent:sa_1"]; p.State != process.Running || p.Parent != "turn:turn-3" || p.Name != "audit" {
		t.Fatalf("sub-agent process = %+v", p)
	}
}

func TestProcessesOfABareControllerIsEmpty(t *testing.T) {
	got, err := New(Options{Sink: event.Discard}).Processes()
	if err != nil || len(got) != 0 {
		t.Fatalf("bare controller: %+v err=%v", got, err)
	}
}
