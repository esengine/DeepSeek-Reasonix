package runtimeservice

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"reasonix/internal/billing"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/sessiontelemetry"
)

func TestProjectContextIsSharedSnapshotProjection(t *testing.T) {
	last := &provider.Usage{PromptTokens: 11, CompletionTokens: 7, ReasoningTokens: 3, CacheHitTokens: 5, CacheMissTokens: 2}
	view, err := ProjectContext(ContextSource{
		UsedTokens: 19, WindowTokens: 128, LastUsage: last,
		Telemetry: sessiontelemetry.Snapshot{
			Usage: sessiontelemetry.UsageStats{
				TotalTokens: 101, CompletionTokens: 31, CacheHitTokens: 17, CacheMissTokens: 4,
				RequestCount: 3, ElapsedMs: 900, SessionCost: 1.25, SessionCurrency: "USD",
				Sources: map[string]sessiontelemetry.UsageSourceStats{
					"planner":  {TotalTokens: 9, RequestCount: 1},
					"executor": {TotalTokens: 92, RequestCount: 2, SessionCost: 1.25, SessionCurrency: "USD"},
				},
			},
			ReadFiles: []sessiontelemetry.ReadFileRecord{{Path: "internal/a.go", Turn: 2, Time: 42, Offset: 1, Limit: 10, Truncated: true}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.UsedTokens != 19 || view.WindowTokens != 128 || view.PromptTokens != 11 ||
		view.TotalTokens != 101 || view.SessionCompletionTokens != 31 || view.RequestCount != 3 ||
		view.ElapsedMillis != 900 || view.SessionCost != 1.25 || view.SessionCurrency != "USD" {
		t.Fatalf("context projection = %+v", view)
	}
	if len(view.Sources) != 2 || view.Sources[0].Source != "executor" || view.Sources[1].Source != "planner" {
		t.Fatalf("sorted sources = %+v", view.Sources)
	}
	if len(view.ReadFiles) != 1 || view.ReadFiles[0].Path != "internal/a.go" || view.ReadFiles[0].Offset == nil || *view.ReadFiles[0].Offset != 1 {
		t.Fatalf("read files = %+v", view.ReadFiles)
	}

	_, err = ProjectContext(ContextSource{Telemetry: sessiontelemetry.Snapshot{
		ReadFiles: []sessiontelemetry.ReadFileRecord{{Path: "../secret", Time: 1}},
	}})
	if !errors.Is(err, ErrInvalidStatusProjection) {
		t.Fatalf("unsafe read file error = %v", err)
	}
}

func TestPageJobsBindsOpaqueCursorToTargetIncarnationAndRevision(t *testing.T) {
	binding := RuntimeBinding{
		Session:     runtimeapi.SessionRef{WorkspaceID: "workspace-a", SessionID: "session-a"},
		Incarnation: "runtime-a",
	}
	items := make([]jobs.View, 205)
	for i := range items {
		items[i] = jobs.View{
			ID: fmt.Sprintf("job-%03d", 204-i), Kind: "bash", Label: fmt.Sprintf("job %d", i),
			Status: "running", StartedAt: int64(204 - i),
		}
	}
	first, err := PageJobs(binding, items, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Jobs) != runtimeapi.PageDefaultItems || !first.HasMore || first.Next == "" || first.Jobs[0].ID != "job-000" {
		t.Fatalf("first page = len %d, more %v, next %q, first %+v", len(first.Jobs), first.HasMore, first.Next, first.Jobs[0])
	}
	second, err := PageJobs(binding, items, first.Next, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Jobs) != 5 || second.HasMore || second.Next != "" || second.Jobs[0].ID != "job-200" {
		t.Fatalf("second page = %+v", second)
	}

	tampered := []byte(first.Next)
	if tampered[len(tampered)-1] == 'A' {
		tampered[len(tampered)-1] = 'B'
	} else {
		tampered[len(tampered)-1] = 'A'
	}
	if _, err := PageJobs(binding, items, runtimeapi.Cursor(tampered), 0); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("tampered cursor error = %v", err)
	}
	other := binding
	other.Session.SessionID = "session-b"
	if _, err := PageJobs(other, items, first.Next, 0); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cross-target cursor error = %v", err)
	}
	changed := append([]jobs.View(nil), items...)
	changed[0].Label = "changed"
	if _, err := PageJobs(binding, changed, first.Next, 0); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("changed-revision cursor error = %v", err)
	}
	if _, err := PageJobs(binding, items, "", runtimeapi.PageMaxItems+1); err == nil {
		t.Fatal("oversize page limit accepted")
	}
}

func TestProjectJobsAndBalanceExposeOnlyFrozenStates(t *testing.T) {
	values, err := ProjectJobs([]jobs.View{
		{ID: "task-2", Kind: "task", Label: "second", Status: "running", StartedAt: 20},
		{ID: "bash-1", Kind: "bash", Label: "first", Status: "running", StartedAt: 10},
	})
	if err != nil || len(values) != 2 || values[0].ID != "bash-1" || values[1].Kind != runtimeapi.JobTask {
		t.Fatalf("jobs = %+v, %v", values, err)
	}
	if _, err := ProjectJobs([]jobs.View{{ID: "done", Kind: "bash", Label: "done", Status: "done"}}); !errors.Is(err, ErrInvalidStatusProjection) {
		t.Fatalf("terminal job error = %v", err)
	}

	available := ProjectBalance(&billing.Balance{Available: true, Infos: []billing.Info{{Currency: "CNY", TotalBalance: "9.50"}}}, nil)
	if !available.Available || available.Display != "¥9.50" {
		t.Fatalf("available balance = %+v", available)
	}
	providerErr := errors.New("https://provider.invalid secret-token")
	if got := ProjectBalance(&billing.Balance{Available: true}, providerErr); !reflect.DeepEqual(got, runtimeapi.BalanceView{}) {
		t.Fatalf("provider error leaked into balance = %+v", got)
	}
}
