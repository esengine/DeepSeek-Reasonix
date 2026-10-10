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

func TestEffectMarkdownOnlyTurnInCodeWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     bool
		turns    []testutil.Turn
		wantPath string
		debt     bool
	}{
		{name: "README only", turns: []testutil.Turn{call("readme", "write_file", `{"path":"README.md","content":"A neutral note.\n"}`)}},
		{name: "README then code", code: true, turns: []testutil.Turn{
			call("readme", "write_file", `{"path":"README.md","content":"A neutral note.\n"}`),
			call("src", "write_file", `{"path":"Program.cs","content":"class P {}\n"}`),
		}, wantPath: "Program.cs", debt: true},
		{name: "failed bash edits code then README", turns: []testutil.Turn{
			call("b", "bash", `{"command":"echo 'class Q {}' >> Program.cs && exit 3"}`),
			call("readme", "write_file", `{"path":"README.md","content":"A neutral note.\n"}`),
		}, wantPath: "could not establish", debt: true},
		{name: "successful unscoped bash edits code then README", turns: []testutil.Turn{
			call("b", "bash", `{"command":"echo 'class Q {}' >> Program.cs"}`),
			call("readme", "write_file", `{"path":"README.md","content":"A neutral note.\n"}`),
		}, wantPath: "could not establish", debt: true},
		{name: "failed named write to code then README", turns: []testutil.Turn{
			call("bad", "write_file", `{"path":"Program.cs/x.cs","content":"class Q {}\n"}`),
			call("readme", "write_file", `{"path":"README.md","content":"A neutral note.\n"}`),
		}, wantPath: "x.cs", debt: true},
		{name: "code only", code: true, turns: []testutil.Turn{call("src", "write_file", `{"path":"Program.cs","content":"class P {}\n"}`)}, wantPath: "Program.cs", debt: true},
		{name: "code renamed to prose", code: true, turns: []testutil.Turn{call("mv", "move_file", `{"source_path":"Program.cs","destination_path":"Program.md"}`)}, wantPath: "Program.cs", debt: true},
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
			writeFile(t, dir, "App.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\" />\n")
			writeFile(t, dir, "Program.cs", "class P {}\n")
			writeFile(t, dir, "README.md", "A neutral readme.\n")
			approveWorkspace(t, dir)
			prov := testutil.NewMock("markdown-only", append(slices.Clone(tc.turns), testutil.Turn{Text: "done"})...)
			setBootTokenProfileTestProvider(t, prov)
			ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, AgentPreset: AgentPresetBalanced, Sink: &bundleAuditSink{}, HeadlessApprovalMode: control.ToolApprovalAuto})
			if err != nil {
				t.Fatal(err)
			}
			defer ctrl.Close()
			err = ctrl.Run(context.Background(), "update the docs")
			var unready *agent.FinalReadinessError
			if !tc.debt {
				if err != nil {
					t.Fatalf("Run = %v, want completion", err)
				}
				return
			}
			if !errors.As(err, &unready) || !slices.Contains(unready.Missing, "verification") {
				t.Fatalf("Run = %v, want a verification readiness error", err)
			}
			if !strings.Contains(unready.Reason, tc.wantPath) {
				t.Errorf("reason %q does not name %s", unready.Reason, tc.wantPath)
			}
		})
	}
}
