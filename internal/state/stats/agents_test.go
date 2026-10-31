package stats

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/usagecatalog"
)

// Calls made from a team agent's seat are listed under that agent, the same on
// both read paths; calls off the team count toward the whole and are listed
// under no one, and a turn marker adds no tokens.
func TestUsageIsListedByAgentOnBothReadPaths(t *testing.T) {
	ctx := context.Background()
	dir := testenv.TempDir(t)
	auditWriteDay(t, dir, "2026-08-01",
		map[string]any{"ts": "2026-08-01T02:00:00Z", "model": "deepseek/chat", "source": "serve", "agent": "a-1", "total": 100, "requests": 2},
		map[string]any{"ts": "2026-08-01T03:00:00Z", "model": "deepseek/chat", "source": "serve", "agent": "a-2", "total": 50, "requests": 1},
		map[string]any{"ts": "2026-08-01T04:00:00Z", "model": "deepseek/chat", "source": "serve", "total": 50, "requests": 1},
		map[string]any{"ts": "2026-08-01T05:00:00Z", "source": "serve", "agent": "a-1", "turn": true},
	)
	auditWriteDay(t, dir, "2026-09-01",
		map[string]any{"ts": "2026-09-01T02:00:00Z", "model": "deepseek/chat", "source": "serve", "agent": "a-2", "total": 100, "requests": 1},
	)
	want := []AgentUsage{{Agent: "a-2", Tokens: 150, Requests: 2, Percent: 50}, {Agent: "a-1", Tokens: 100, Requests: 2, Percent: 100.0 / 3}}

	check := func(path string, got []AgentUsage) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: agents = %+v", path, got)
		}
		for i := range want {
			if got[i].Agent != want[i].Agent || got[i].Tokens != want[i].Tokens || got[i].Requests != want[i].Requests || math.Abs(got[i].Percent-want[i].Percent) > 0.01 {
				t.Fatalf("%s: agents = %+v, want %+v", path, got, want)
			}
		}
	}
	check("records", auditQuery(t, dir, nil).Agents)

	catalog, err := usagecatalog.Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if !catalog.Ready(ctx, dir, daysInRange(auditFilter().From, auditFilter().To)) {
		t.Fatal("the projection is not ready, so the query would read the records instead")
	}
	check("projection", auditQuery(t, dir, catalog).Agents)

	single := auditFilter()
	single.From, single.To = time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local), time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	day, err := NewWriter(dir).Query(single)
	if err != nil || len(day.Agents) != 2 || day.Agents[0].Agent != "a-1" || day.Agents[0].Tokens != 100 {
		t.Fatalf("one day = %+v, %v", day.Agents, err)
	}
}

func TestARecorderForAnAgentLabelsWhatItRecords(t *testing.T) {
	dir := testenv.TempDir(t)
	rec := NewRecorder(event.Discard, dir, "serve").ForAgent("a-7")
	rec.Emit(event.Event{Kind: event.Usage, ModelRef: "deepseek/chat", Usage: &provider.Usage{PromptTokens: 9, CompletionTokens: 1, TotalTokens: 10}})
	rec.RecordTurnCompletion()
	if err := rec.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, time.Now().Format(dayLayout)+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(data, []byte(`"agent":"a-7"`)); n != 2 {
		t.Fatalf("%d of the records name the agent: %s", n, data)
	}
}
