package installsource

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/safety/permission"
)

// writeHookedPlugin lays out a package whose hooks execute during sessions,
// which is what grades a plan high.
func writeHookedPlugin(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "reasonix-plugin.json"), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "ticketed",
  "version": "1.0.0",
  "skills": ["skills"],
  "hooks": {"SessionStart": [{"command": "hooks/start.sh"}]}
}`)
	writeFile(t, filepath.Join(root, "skills", "greet", "SKILL.md"),
		"---\nname: greet\ndescription: say hello\n---\nHello")
	writeFile(t, filepath.Join(root, "hooks", "start.sh"), "#!/bin/sh\n")
}

func applyArgs(t *testing.T, source, planID string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"source": source, "kind": "plugin", "apply": true, "planId": planID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The permission layer refuses to auto-approve a plan graded high, and this
// tool is what writes that grade into the ticket. The two live in packages that
// cannot import each other, so the agreement is only real here.
func TestHighRiskPlanAsksEvenUnderBlanketAllow(t *testing.T) {
	src := testenv.TempDir(t)
	writeHookedPlugin(t, src)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t), RequireApprovedPlan: true})

	resp := execInstall(t, tl, map[string]any{"source": src, "kind": "plugin"})
	if !resp.OK || resp.Status != "planned" {
		t.Fatalf("plan = %+v", resp)
	}
	if !strings.HasPrefix(resp.PlanID, "high:") {
		t.Fatalf("planId = %q, want the plan's grade in front of the hash", resp.PlanID)
	}

	// "allow everything" is the yolo posture: it covers this workspace's files,
	// not the agent handing itself a lifecycle hook.
	blanket := permission.New("allow", nil, nil, nil)
	if got := blanket.Decide("install_source", false, applyArgs(t, src, resp.PlanID)); got != permission.Ask {
		t.Fatalf("blanket allow decided %v for a high-risk self-extension, want Ask", got)
	}
	// A user who means it moves the line with a rule; the blanket alone does not.
	explicit := permission.New("allow", []string{"install_source(high:*)"}, nil, nil)
	if got := explicit.Decide("install_source", false, applyArgs(t, src, resp.PlanID)); got != permission.Allow {
		t.Fatalf("an explicit rule for the ticket decided %v, want Allow", got)
	}
}

// Previewing is a read: it reports what a source contains and writes nothing.
func TestPlanOnlyCallIsAReadEvenInAskMode(t *testing.T) {
	src := testenv.TempDir(t)
	writeHookedPlugin(t, src)
	raw, err := json.Marshal(map[string]any{"source": src, "kind": "plugin"})
	if err != nil {
		t.Fatal(err)
	}
	ask := permission.New("ask", nil, nil, nil)
	gate := permission.NewGate(ask, nil)
	if !permission.InstallSourceIsPlanOnly(raw) {
		t.Fatal("a call with no apply was not recognized as a preview")
	}
	if allow, reason, err := gate.Check(t.Context(), "install_source", raw, false); err != nil || !allow {
		t.Fatalf("preview refused: allow=%v reason=%q err=%v", allow, reason, err)
	}
}

// Without a ticket the apply is answered with the plan it should have read.
// Nothing reaches disk, and the caller gets exactly what it was missing.
func TestApplyWithoutTicketInstallsNothing(t *testing.T) {
	src := testenv.TempDir(t)
	writeHookedPlugin(t, src)
	home := testenv.TempDir(t)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home, RequireApprovedPlan: true})

	resp := execInstall(t, tl, map[string]any{"source": src, "kind": "plugin", "apply": true})
	if resp.Applied || resp.Status != "planned" {
		t.Fatalf("response = %+v, want a plan and no write", resp)
	}
	if !strings.Contains(resp.Next, "planId") {
		t.Fatalf("next = %q, want it to name what the call was missing", resp.Next)
	}
	reasonixHome := filepath.Join(home, ".reasonix")
	if _, err := os.Stat(pluginpkg.InstallRoot(reasonixHome, "ticketed")); !os.IsNotExist(err) {
		t.Fatalf("an unticketed apply wrote an install root: %v", err)
	}

	// The ticket it handed back is the one that works.
	done := execInstall(t, tl, map[string]any{
		"source": src, "kind": "plugin", "apply": true, "planId": resp.PlanID,
	})
	if !done.OK || done.Status != "done" {
		t.Fatalf("ticketed apply = %+v", done)
	}
}

// The grade is not a label the caller gets to choose. A ticket that claims a
// lower one is a different string than the plan hashes to, and the apply is
// refused before anything is written.
func TestDowngradedTicketIsRefused(t *testing.T) {
	src := testenv.TempDir(t)
	writeHookedPlugin(t, src)
	home := testenv.TempDir(t)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: home, RequireApprovedPlan: true})

	resp := execInstall(t, tl, map[string]any{"source": src, "kind": "plugin"})
	forged := strings.Replace(resp.PlanID, "high:", "low:", 1)
	if forged == resp.PlanID {
		t.Fatalf("planId = %q, expected a high grade to rewrite", resp.PlanID)
	}
	raw, err := json.Marshal(map[string]any{
		"source": src, "kind": "plugin", "apply": true, "planId": forged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tl.Execute(t.Context(), raw); err == nil {
		t.Fatal("a downgraded ticket was accepted")
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(filepath.Join(home, ".reasonix"), "ticketed")); !os.IsNotExist(err) {
		t.Fatalf("a refused apply still wrote an install root: %v", err)
	}
}

// Hosts install on the user's own click, so they never gained a second phase
// they would have had to invent a ticket for.
func TestHostToolAppliesWithoutATicket(t *testing.T) {
	src := testenv.TempDir(t)
	writeHookedPlugin(t, src)
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	resp := execInstall(t, tl, map[string]any{"source": src, "kind": "plugin", "apply": true})
	if !resp.OK || resp.Status != "done" {
		t.Fatalf("host apply = %+v", resp)
	}
}
