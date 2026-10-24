package boot

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

func TestEffectDeliveryOwesNoVerificationForProseOutsideAnyRepository(t *testing.T) {
	for _, tc := range []struct {
		name string
		repo bool
		debt bool
	}{
		{name: "bare folder"},
		{name: "inside a repository", repo: true, debt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			home, err := filepath.EvalSymlinks(robustTempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("REASONIX_HOME", home)
			dir, err := filepath.EvalSymlinks(robustTempDir(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			writeUserConfig(t, userModel+"\n[codegraph]\nenabled = false\n")
			registerBootTokenProfileTestProvider()
			if tc.repo {
				initEffectRepository(t, dir)
			}
			approveWorkspace(t, dir)
			prov := testutil.NewMock("unversioned-prose",
				testutil.Turn{Text: "[]"},
				call("todo", "todo_write", `{"todos":[{"content":"Write a neutral note","status":"in_progress"}]}`),
				call("notes", "write_file", `{"path":"notes.md","content":"A neutral note.\n"}`),
				testutil.Turn{Text: "done"})
			setBootTokenProfileTestProvider(t, prov)
			ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, AgentPreset: AgentPresetDelivery, Sink: &bundleAuditSink{}, HeadlessApprovalMode: control.ToolApprovalAuto})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			err = ctrl.Run(context.Background(), "write the note")
			var unready *agent.FinalReadinessError
			if !errors.As(err, &unready) {
				t.Fatalf("Run = %v, want a readiness error: delivery still owes sign-off and inspection", err)
			}
			if got := slices.Contains(unready.Missing, "verification"); got != tc.debt {
				t.Errorf("missing = %v, verification owed = %v, want %v", unready.Missing, got, tc.debt)
			}
			result := toolResults(prov.Requests())["notes"]
			if got := strings.Contains(result, "stale_verification"); got != tc.debt {
				t.Errorf("stale_verification in the write's result = %v, want %v; result: %s", got, tc.debt, result)
			}
		})
	}
}
