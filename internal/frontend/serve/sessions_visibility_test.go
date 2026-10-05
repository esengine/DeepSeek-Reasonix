package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func listSessionNames(t *testing.T, ctrl *control.Controller) []string {
	t.Helper()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(got))
	for _, entry := range got {
		names = append(names, entry.Name)
	}
	return names
}

func saveVisibilitySession(t *testing.T, path string, messages ...string) {
	t.Helper()
	s := sessionstore.NewSession("sys")
	for i, message := range messages {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		s.Add(provider.Message{Role: role, Content: message})
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

}

// A save-conflict loop forks one recovery branch per save — a second runtime
// writing the same session produced ten, and the sidebar showed ten entries of
// one conversation, all with the same generated title.
func TestSessionsFoldsCoveredRecoveryCopies(t *testing.T) {
	dir := testenv.TempDir(t)
	root := filepath.Join(dir, "20260815-161507-deepseek-v4-flash.jsonl")
	saveVisibilitySession(t, root, "今日热点", "好的")
	for range 3 {
		fork := sessionstore.NewSession("sys")
		fork.Add(provider.Message{Role: provider.RoleUser, Content: "今日热点"})
		if _, err := fork.SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: root}); err != nil {
			t.Fatal(err)
		}
	}
	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	ctrl.SetSessionPath(root)

	names := listSessionNames(t, ctrl)
	if len(names) != 1 || names[0] != "20260815-161507-deepseek-v4-flash" {
		t.Fatalf("GET /sessions = %v, want only the parent session", names)
	}
}

// A branch that was continued on, or that the controller is writing to, holds
// conversation the parent does not: hiding it would lose the user's work.
func TestSessionsKeepsRecoveryBranchesThatHoldContent(t *testing.T) {
	dir := testenv.TempDir(t)
	root := filepath.Join(dir, "20260815-161507-deepseek-v4-flash.jsonl")
	saveVisibilitySession(t, root, "今日热点", "好的")

	diverged := sessionstore.NewSession("sys")
	diverged.Add(provider.Message{Role: provider.RoleUser, Content: "今日热点"})
	divergedInfo, err := diverged.SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatal(err)
	}
	diverged.Add(provider.Message{Role: provider.RoleAssistant, Content: "只有分支里有的回答"})
	if err := diverged.Save(divergedInfo.Path); err != nil {
		t.Fatal(err)
	}

	active := sessionstore.NewSession("sys")
	active.Add(provider.Message{Role: provider.RoleUser, Content: "今日热点"})
	activeInfo, err := active.SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatal(err)
	}

	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	ctrl.SetSessionPath(activeInfo.Path)

	names := listSessionNames(t, ctrl)
	want := map[string]bool{
		filepath.Base(strings.TrimSuffix(root, ".jsonl")):              true,
		filepath.Base(strings.TrimSuffix(divergedInfo.Path, ".jsonl")): true,
		filepath.Base(strings.TrimSuffix(activeInfo.Path, ".jsonl")):   true,
	}
	if len(names) != len(want) {
		t.Fatalf("GET /sessions = %v, want the parent, the continued branch and the open branch", names)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("GET /sessions = %v, unexpected entry %q", names, name)
		}
	}
}

// A subagent transcript is a task's internal trace, not one of the user's
// conversations. Listing them buried the real sessions: 154 of 189 entries in a
// real profile were subagents.
func TestSessionsExcludesSubagentTranscripts(t *testing.T) {
	dir := testenv.TempDir(t)
	for _, name := range []string{
		"20260813-235122-deepseek-v4-flash.jsonl",
		"subagent-sub-k-202605171512.jsonl",
		"subagent-sub-1-202605171456.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	ctrl.SetSessionPath(filepath.Join(dir, "20260813-235122-deepseek-v4-flash.jsonl"))

	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("GET /sessions returned %d entries, want only the real session: %+v", len(got), got)
	}
	if got[0].Name != "20260813-235122-deepseek-v4-flash" {
		t.Errorf("listed %q, want the real session", got[0].Name)
	}
}
