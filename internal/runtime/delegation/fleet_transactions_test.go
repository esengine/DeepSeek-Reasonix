package delegation

import (
	"path/filepath"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func TestFleetOverlappingGrantsUseTransactionsWhileLegacyStillRefuses(t *testing.T) {
	for _, transactions := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "transactions"}[transactions], func(t *testing.T) {
			root := t.TempDir()
			task := NewTaskTool(&mockProvider{name: "fleet-transactions"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).
				WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "")
			if transactions {
				coordinator, err := mutation.New(root, t.TempDir(), nil)
				if err != nil {
					t.Fatal(err)
				}
				task.WithMutationCoordinator(coordinator)
			}
			call, err := NewFleetTool(task).preflight(testTaskContext(), []fleetTaskItem{
				{Prompt: "first", WritePaths: []string{"out"}},
				{Prompt: "second", WritePaths: []string{"out"}},
			}, false)
			if !transactions {
				if err == nil {
					t.Fatal("legacy overlapping reservations accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("transaction grants treated as task reservations: %v", err)
			}
			for _, spec := range call.specs {
				if !spec.Grant.WritePaths.AllowsPath(filepath.Join(root, "out", "file.md")) || spec.Grant.WritePaths.AllowsPath(filepath.Join(root, "elsewhere.md")) {
					t.Fatal("transaction admission widened or removed path authorization")
				}
			}
		})
	}
}

func TestFleetParallelWriterIntentExcludesOrderedReadonlyAndAdoptedItems(t *testing.T) {
	for _, ordered := range []bool{false, true} {
		items := []fleetTaskItem{{ID: "a"}, {ID: "b"}, {ID: "reader"}, {ID: "adopted"}}
		if ordered {
			items[1].DependsOn = []string{"a"}
		}
		plan, err := newFleetPlan(items, false)
		if err != nil {
			t.Fatal(err)
		}
		specs := make([]ProfileExecSpec, 4)
		specs[2].Grant.ReadOnly = true
		plan.markParallelWriters(specs, map[int]adoptedItem{3: {}})
		for i, spec := range specs {
			want := !ordered && i < 2
			if spec.Sched.ParallelWriter != want {
				t.Fatalf("ordered=%v item=%d parallel=%v want=%v", ordered, i, spec.Sched.ParallelWriter, want)
			}
		}
	}
}
