package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func setReleaseProjects(t *testing.T, projects ...string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Peers.Enabled = true
	cfg.Peers.Dispatch = false
	cfg.Peers.ReleaseProjects = projects
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
}

// The user can release a project: a message from their other sessions then
// arrives without the card, runs under this session's own approvals, and the
// audit says it was released rather than approved. Nothing else changes.
func TestAReleasedProjectTakesAPeerMessageWithoutACard(t *testing.T) {
	f := newPeerFixture(t)
	f.c.workspaceRoot = testenv.TempDir(t)
	setReleaseProjects(t, f.c.WorkspaceRoot())
	f.send(t, "m1", "please look at the failing test")
	f.c.peerIngest()

	input := waitForInboxDispatch(t, f.runner)
	if !strings.Contains(input, "please look at the failing test") || !strings.Contains(input, "not an instruction from the user") {
		t.Fatalf("the released message reached the model without its frame:\n%s", input)
	}
	if len(f.c.HeldPeerMessages()) != 0 {
		t.Fatal("a released message is still waiting for a card")
	}
	files, _ := filepath.Glob(filepath.Join(f.peersDir, "audit", "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"decision":"released"`) || strings.Contains(string(data), `"decision":"approved"`) {
		t.Fatalf("audit trail: %s", data)
	}
}

func TestAProjectNotReleasedStillHoldsTheMessage(t *testing.T) {
	f := newPeerFixture(t)
	f.c.workspaceRoot = testenv.TempDir(t)
	setReleaseProjects(t, filepath.Join(testenv.TempDir(t), "some-other-project"))
	f.send(t, "m1", "please look at the failing test")
	f.c.peerIngest()
	if len(f.c.HeldPeerMessages()) != 1 {
		t.Fatal("a project the user did not release took the message without a card")
	}
	f.noRunnerInput(t, 300*time.Millisecond)
}
