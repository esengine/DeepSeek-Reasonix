package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func newSchemaTwoChatTUI(t *testing.T) (chatTUI, *control.Controller, *agent.Session, string) {
	t.Helper()
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "root answer"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test", Sink: event.Discard})
	path := filepath.Join(dir, "root.jsonl")
	ctrl.SetSessionPath(path)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	return m, ctrl, sess, path
}

func TestBranchAndSwitchCommandsMoveBetweenHeadsInPlace(t *testing.T) {
	m, ctrl, sess, path := newSchemaTwoChatTUI(t)
	m.runBranchCommand("/branch experiment")
	if ctrl.SessionPath() != path {
		t.Fatalf("/branch moved the session to %q, want the same log", ctrl.SessionPath())
	}
	tree := strings.Join(*m.pendingCommit, "\n")
	if !strings.Contains(tree, "experiment") || !strings.Contains(tree, "current") {
		t.Fatalf("/branch tree must list the new head as current:\n%s", tree)
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "on the branch"})
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}
	m.runSwitchCommand("/switch " + agent.BranchID(path))
	if got := len(ctrl.History()); got != 3 || !m.sessionSwitch || ctrl.SessionPath() != path {
		t.Fatalf("/switch to main: history %d switch %v path %q", got, m.sessionSwitch, ctrl.SessionPath())
	}
	m.sessionSwitch = false
	m.runSwitchCommand("/switch experiment")
	if got := ctrl.History(); len(got) != 4 || got[3].Content != "on the branch" || !m.sessionSwitch {
		t.Fatalf("/switch by name: history %d switch %v", len(got), m.sessionSwitch)
	}
	if heads, err := agent.ListSessionHeads(path); err != nil || len(heads) != 2 || !heads[1].Selected {
		t.Fatalf("heads = %+v err=%v", heads, err)
	}
}

func TestSessionsDiagnoseCountsSessionLogHeadsAndCleanupLeavesThem(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	s := agent.NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "shared question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "shared answer"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ForkHead(path, s.Snapshot()[2].ID, agent.HeadKindFork, "alt"); err != nil {
		t.Fatal(err)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "alt question"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) sessionRecoveryReport {
		t.Helper()
		var report sessionRecoveryReport
		out := captureStdout(t, func() {
			if rc := sessionsRecoveryCommand(append([]string{"--dir", dir, "--json"}, args...), true); rc != 0 {
				t.Fatalf("sessions cleanup rc = %d", rc)
			}
		})
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("report %q: %v", out, err)
		}
		return report
	}
	dry := run()
	if dry.SourceSessions != 1 || dry.SessionLogs != 1 || dry.Heads != 2 || dry.CoveredHeads != 1 || dry.RetiredHeads != 0 {
		t.Fatalf("dry-run report = %+v", dry)
	}
	if dry.Groups != 0 || dry.CleanupEligible != 0 || !dry.DryRun {
		t.Fatalf("a session log must not form a recovery group: %+v", dry)
	}
	applied := run("--apply")
	if applied.MovedToTrash != 0 || applied.Busy != 0 || applied.DryRun {
		t.Fatalf("cleanup --apply on a session log = %+v, want nothing moved", applied)
	}
	heads, err := agent.ListSessionHeads(path)
	if err != nil || len(heads) != 2 || heads[0].Retired || heads[1].Retired {
		t.Fatalf("heads after cleanup = %+v err=%v, want the log untouched", heads, err)
	}
}
