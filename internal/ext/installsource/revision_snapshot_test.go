package installsource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRevisionUnchangedPartialCapabilityTicket(t *testing.T) {
	tool, source := revisionPlugin(t)
	if err := os.Mkdir(filepath.Join(source, ".mcp.json"), 0755); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
	first := execInstall(t, tool, args)
	second := execInstall(t, tool, args)
	if first.PlanID != second.PlanID {
		t.Fatalf("unchanged partial package moved ticket: %s / %s", first.PlanID, second.PlanID)
	}
	args["apply"], args["planId"] = true, first.PlanID
	if done, err := execRaw(t, tool, args); err != nil || done.Status != "done" {
		t.Fatalf("unchanged partial copy refused: %+v, %v", done, err)
	}
}
