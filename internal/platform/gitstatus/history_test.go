package gitstatus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// A repository with a side branch that merges back: the shape a graph exists to
// draw, so a commit has two parents and two branches carry commits of their own.
func mergedHistory(t *testing.T) string {
	t.Helper()
	dir := repoWithCommit(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	here, _ := Summary(context.Background(), opened(t, dir))
	main := here.Branch
	git(t, dir, "checkout", "-q", "-b", "side")
	write("side.go", "package a\n")
	git(t, dir, "add", "side.go")
	git(t, dir, "commit", "-qm", "side work")
	git(t, dir, "checkout", "-q", main)
	write("main.go", "package a\n")
	git(t, dir, "add", "main.go")
	git(t, dir, "commit", "-qm", "main work")
	git(t, dir, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	git(t, dir, "tag", "v1")
	return dir
}

func TestHistoryListsCommitsNewestFirstWithParentsAndRefs(t *testing.T) {
	dir := mergedHistory(t)
	page, ok, err := History(context.Background(), opened(t, dir), 50)
	if err != nil || !ok {
		t.Fatalf("History: %v ok=%v", err, ok)
	}
	var subjects []string
	for _, c := range page.Commits {
		subjects = append(subjects, c.Subject)
	}
	if got := strings.Join(subjects, "|"); !strings.HasPrefix(got, "merge side|") || !strings.Contains(got, "side work") || !strings.Contains(got, "main work") {
		t.Fatalf("subjects = %v, want the merge first and both sides present", subjects)
	}
	merge := page.Commits[0]
	if len(merge.Parents) != 2 {
		t.Fatalf("merge parents = %v, want two", merge.Parents)
	}
	byID := map[string]Commit{}
	for _, c := range page.Commits {
		byID[c.ID] = c
	}
	for _, p := range merge.Parents {
		if _, known := byID[p]; !known {
			t.Fatalf("parent %s is not in the page", p)
		}
	}
	joined := strings.Join(merge.Refs, ",")
	if !strings.Contains(joined, "tag: v1") || !strings.Contains(joined, "HEAD") {
		t.Fatalf("refs on the tip = %v, want HEAD and the tag", merge.Refs)
	}
	if merge.Author == "" || merge.At == 0 {
		t.Fatalf("commit = %+v, want an author and a time", merge)
	}
	if page.Truncated {
		t.Fatal("a four-commit history was reported as cut")
	}
}

func TestHistoryIsBoundedAndSaysSo(t *testing.T) {
	dir := mergedHistory(t)
	page, ok, err := History(context.Background(), opened(t, dir), 2)
	if err != nil || !ok {
		t.Fatalf("History: %v ok=%v", err, ok)
	}
	if len(page.Commits) != 2 || !page.Truncated {
		t.Fatalf("got %d commits truncated=%v, want 2 and truncated", len(page.Commits), page.Truncated)
	}
	if page, _, _ := History(context.Background(), opened(t, dir), 100000); len(page.Commits) > maxHistory {
		t.Fatalf("the cap is %d, got %d", maxHistory, len(page.Commits))
	}
}

func TestHistoryOfAnEmptyOrUnversionedWorkspaceIsNotAnError(t *testing.T) {
	if _, ok, err := History(context.Background(), opened(t, testenv.TempDir(t)), 10); ok || err != nil {
		t.Fatalf("not a repository: ok=%v err=%v, want a quiet false", ok, err)
	}
	empty := testenv.TempDir(t)
	git(t, empty, "init", "-q")
	page, ok, err := History(context.Background(), opened(t, empty), 10)
	if err != nil || !ok || len(page.Commits) != 0 {
		t.Fatalf("no commits yet: %+v ok=%v err=%v, want an empty page", page, ok, err)
	}
}

// A subject is the commit author's text: control characters and length never
// reach a caller as they were written.
func TestHistoryCleansAndBoundsTheSubject(t *testing.T) {
	dir := repoWithCommit(t)
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "evil\x1b[31m red "+strings.Repeat("x", 400))
	page, _, err := History(context.Background(), opened(t, dir), 5)
	if err != nil || len(page.Commits) == 0 {
		t.Fatalf("History: %v %+v", err, page)
	}
	s := page.Commits[0].Subject
	if strings.ContainsRune(s, '\x1b') || len([]rune(s)) > maxSubject {
		t.Fatalf("subject = %q (%d runes)", s, len([]rune(s)))
	}
}
