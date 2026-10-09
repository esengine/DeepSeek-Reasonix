package installsource

import (
	"context"
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"testing"
)

func TestRevisionRemotePartialCapabilityTicketStable(t *testing.T) {
	tool, _ := revisionPlugin(t)
	tool.preparePlugin = func(context.Context, string, string) (string, string, func(), error) {
		root := testenv.TempDir(t)
		writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"approved"}`)
		writeFile(t, filepath.Join(root, "skills", "neutral", "SKILL.md"), "---\ndescription: Neutral fixture\n---\nBODY")
		if err := os.Mkdir(filepath.Join(root, ".mcp.json"), 0755); err != nil {
			t.Fatal(err)
		}
		return root, pinnedCommit, func() {}, nil
	}
	args := map[string]any{"source": "https://github.com/neutral/approved", "kind": "plugin"}
	first := execInstall(t, tool, args)
	second := execInstall(t, tool, args)
	if first.PlanID != second.PlanID {
		t.Fatalf("identical remote snapshots have different tickets: %s / %s", first.PlanID, second.PlanID)
	}
	args["apply"], args["planId"] = true, first.PlanID
	if done, err := execRaw(t, tool, args); err != nil || done.Status != "done" {
		t.Fatalf("unchanged remote copy: %+v, %v", done, err)
	}
}
