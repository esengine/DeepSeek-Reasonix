package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/observe"
	"reasonix/internal/runtime/agent/testutil"
)

const storeMark = "SCHEDULE-STORE-BODY"

// The store sits inside the workspace here, so the read scope alone would let
// the run see it; the protected set is what keeps a run from reading its own
// limits and the results of the other schedules.
func TestEffectObserveCannotReadTheScheduleStore(t *testing.T) {
	root := observeProject(t)
	state := filepath.Join(root, "state")
	t.Setenv("REASONIX_STATE_HOME", state)
	store := filepath.Join(state, "schedules")
	writeFile(t, store, "schedules.json", `{"note":"`+storeMark+`"}`)
	writeFile(t, filepath.Join(store, "results"), "r.json", storeMark)
	writeFile(t, root, "inside.go", "package in\n// "+insideMark+"\n")
	slash := filepath.ToSlash(store)
	calls := [][3]string{
		{"read", "read_file", `{"path":"` + slash + `/schedules.json"}`},
		{"read-rel", "read_file", `{"path":"state/schedules/results/r.json"}`},
		{"ls", "ls", `{"path":"` + slash + `"}`},
		{"glob", "glob", `{"pattern":"state/schedules/**/*.json"}`},
		{"grep-dir", "grep", `{"pattern":"` + storeMark + `","path":"state/schedules"}`},
		{"grep-walk", "grep", `{"pattern":"` + storeMark + `","path":"."}`},
		{"idx", "code_index", `{"action":"outline","path":"state/schedules"}`},
		{"ok", "read_file", `{"path":"inside.go"}`},
	}
	var turns []testutil.Turn
	for _, c := range calls {
		turns = append(turns, call(c[0], c[1], c[2]))
	}
	prov := testutil.NewMock("observe", append(turns, testutil.Turn{Text: "done"})...)
	ctrl, _ := buildObserved(t, root, prov, observe.RunContext{ScheduleID: "s", TriggerID: "t"})
	if err := ctrl.Run(context.Background(), "read"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	results := toolResults(prov.Requests())
	for _, c := range calls[:len(calls)-1] {
		if strings.Contains(results[c[0]], storeMark) {
			t.Errorf("%s read the schedule store: %q", c[0], results[c[0]])
		}
	}
	if !strings.Contains(results["ok"], insideMark) {
		t.Fatalf("the control read of an ordinary file failed, so the refusals prove nothing: %q", results["ok"])
	}
}

// A retired key in the workspace's config is what the ordinary boot rewrites in
// place; a read-only run must leave the file, and the user's own, as it found them.
func TestEffectObserveWritesNoConfigFile(t *testing.T) {
	root := observeProject(t)
	project := "[agent]\nmax_steps = 7\n\n[secrets]\nredact_tool_output = true\n"
	writeFile(t, root, "reasonix.toml", project)
	prov := testutil.NewMock("observe", testutil.Turn{Text: "done"})
	ctrl, _ := buildObserved(t, root, prov, observe.RunContext{ScheduleID: "s", TriggerID: "t"})
	if err := ctrl.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "reasonix.toml"))
	if err != nil || string(got) != project {
		t.Fatalf("the workspace config changed under the read-only posture: %q, %v", got, err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 {
		t.Fatalf("the workspace gained files: %v", entries)
	}
}

// The protected set masks only a directory that exists when a command's sandbox
// is built, so a build must leave the store's directory behind.
func TestBuildCreatesTheScheduleStoreDirectoryPrivately(t *testing.T) {
	root := observeProject(t)
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(root, "state"))
	prov := testutil.NewMock("observe", testutil.Turn{Text: "done"})
	buildObserved(t, root, prov, observe.RunContext{ScheduleID: "s", TriggerID: "t"})
	info, err := os.Stat(filepath.Join(root, "state", "schedules"))
	if err != nil || !info.IsDir() {
		t.Fatalf("store directory missing after a build: %v", err)
	}
}
