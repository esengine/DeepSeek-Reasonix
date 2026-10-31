package usagecatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestQueryAgentsSumsTeamCallsBySourceAndRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "2026-08-10.jsonl")
	lines := "" +
		`{"ts":"2026-08-10T10:00:00+08:00","model":"deepseek/m","source":"serve","agent":"a-1","total":10}` + "\n" +
		`{"ts":"2026-08-10T11:00:00+08:00","model":"deepseek/m","source":"serve","agent":"a-2","total":30,"requests":2}` + "\n" +
		`{"ts":"2026-08-10T12:00:00+08:00","model":"deepseek/m","source":"cli","agent":"a-1","total":5}` + "\n" +
		`{"ts":"2026-08-10T13:00:00+08:00","model":"deepseek/m","source":"serve","total":99}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(testenv.TempDir(t), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	all, err := catalog.QueryAgents(ctx, "2026-08-10", "2026-08-10", "")
	if err != nil || len(all) != 2 || all[0] != (AgentRollup{Agent: "a-2", Total: 30, Requests: 2}) || all[1] != (AgentRollup{Agent: "a-1", Total: 15, Requests: 2}) {
		t.Fatalf("all sources = %+v, %v", all, err)
	}
	serve, err := catalog.QueryAgents(ctx, "2026-08-10", "2026-08-10", "serve")
	if err != nil || len(serve) != 2 || serve[1] != (AgentRollup{Agent: "a-1", Total: 10, Requests: 1}) {
		t.Fatalf("serve only = %+v, %v", serve, err)
	}
	if none, err := catalog.QueryAgents(ctx, "2026-08-11", "2026-08-12", ""); err != nil || len(none) != 0 {
		t.Fatalf("outside the range = %+v, %v", none, err)
	}
}
