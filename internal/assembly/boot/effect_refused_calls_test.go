package boot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

func TestEffectRefusedCallReachesTheNextRequestOnce(t *testing.T) {
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
	approveWorkspace(t, dir)
	prov := testutil.NewMock("refused-calls",
		call("w", "write_file", `{"path":"notes.txt","content":"x\n"}`),
		testutil.Turn{Text: "I could not write it."},
		testutil.Turn{Text: "second"},
		testutil.Turn{Text: "third"},
	)
	setBootTokenProfileTestProvider(t, prov)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, AgentPreset: AgentPresetBalanced, HeadlessApprovalMode: control.ToolApprovalDontAsk})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	for _, input := range []string{"write the notes", "anything else?", "and now?"} {
		if err := ctrl.Run(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	var carrying []int
	for i, req := range prov.Requests() {
		last := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(last, "<refused-calls>") && strings.Contains(last, "notes.txt") {
			carrying = append(carrying, i)
		}
	}
	if len(carrying) != 1 {
		t.Fatalf("requests carrying the refusal = %v of %d, want exactly the first one after it", carrying, len(prov.Requests()))
	}
}
