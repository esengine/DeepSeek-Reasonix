package runtimeservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/runtimeapi"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "runtime@example.test")
	runGit(t, root, "config", "user.name", "Runtime Test")
	runGit(t, root, "config", "gc.auto", "0")
	return root
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	raw, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			t.Skip("git is unavailable")
		}
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(raw)))
	}
	return strings.TrimSpace(string(raw))
}

func commitAll(t *testing.T, root, message string) string {
	t.Helper()
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-q", "-m", message)
	return runGit(t, root, "rev-parse", "HEAD")
}

func TestWorkspaceChangesMergesCheckpointAndGitAndSuppressesGitFailure(t *testing.T) {
	root := initGitRepo(t)
	writeTestFile(t, root, "both.txt", []byte("base\n"))
	writeTestFile(t, root, "git-only.txt", []byte("base\n"))
	commitAll(t, root, "base")
	writeTestFile(t, root, "both.txt", []byte("changed\n"))
	writeTestFile(t, root, "git-only.txt", []byte("changed\n"))
	provider := CheckpointChangeProviderFunc(func(context.Context) ([]CheckpointChange, error) {
		return []CheckpointChange{
			{Path: "both.txt", Turn: 2, Prompt: "later", TimeMillis: 20},
			{Path: "both.txt", Turn: 1, Prompt: "earlier", TimeMillis: 10},
			{Path: "session-only.txt", Turn: 3, Prompt: "session", TimeMillis: 30},
		}, nil
	})
	service := newTestService(t, root, provider, "")
	page, err := service.WorkspaceChanges(context.Background(), runtimeapi.WorkspaceChangesInput{Session: testSession, Limit: 2})
	if err != nil {
		t.Fatalf("WorkspaceChanges: %v", err)
	}
	if !page.GitAvailable || len(page.Files) != 2 || !page.HasMore || page.Next == "" {
		t.Fatalf("first changes page = %+v", page)
	}
	if page.Files[0].Path != "both.txt" || fmt.Sprint(page.Files[0].Sources) != "[session git]" || fmt.Sprint(page.Files[0].Turns) != "[1 2]" || page.Files[0].LatestPrompt != "later" {
		t.Fatalf("merged change = %+v", page.Files[0])
	}
	second, err := service.WorkspaceChanges(context.Background(), runtimeapi.WorkspaceChangesInput{Session: testSession, Cursor: page.Next})
	if err != nil || second.HasMore || len(second.Files) != 1 {
		t.Fatalf("second changes page = %+v, err=%v", second, err)
	}

	unavailable := newTestService(t, root, provider, filepath.Join(root, "missing-git"))
	checkpointOnly, err := unavailable.WorkspaceChanges(context.Background(), runtimeapi.WorkspaceChangesInput{Session: testSession})
	if err != nil {
		t.Fatalf("WorkspaceChanges with unavailable Git leaked failure: %v", err)
	}
	if checkpointOnly.GitAvailable || checkpointOnly.GitBranch != "" || len(checkpointOnly.Files) != 2 {
		t.Fatalf("checkpoint-only changes = %+v", checkpointOnly)
	}
}

func TestWorkspaceChangesRejectsUnsafeCheckpointPath(t *testing.T) {
	root := t.TempDir()
	service := newTestService(t, root, CheckpointChangeProviderFunc(func(context.Context) ([]CheckpointChange, error) {
		return []CheckpointChange{{Path: "../outside", Turn: 1}}, nil
	}), filepath.Join(root, "missing-git"))
	_, err := service.WorkspaceChanges(context.Background(), runtimeapi.WorkspaceChangesInput{Session: testSession})
	if !errors.Is(err, ErrPathEscapesRoot) {
		t.Fatalf("unsafe checkpoint err = %v", err)
	}
}

func TestGitHistoryFullHashRFC3339AndLimit(t *testing.T) {
	root := initGitRepo(t)
	for i := 0; i < runtimeapi.GitHistoryCommits+1; i++ {
		writeTestFile(t, root, "history.txt", []byte(fmt.Sprintf("line %d\n", i)))
		commitAll(t, root, fmt.Sprintf("commit-%03d", i))
	}
	service := newTestService(t, root, nil, "")
	history, err := service.GitHistory(context.Background(), runtimeapi.GitHistoryInput{Session: testSession, Path: "history.txt"})
	if err != nil {
		t.Fatalf("GitHistory: %v", err)
	}
	if len(history.Commits) != runtimeapi.GitHistoryCommits || history.ReturnedItems != runtimeapi.GitHistoryCommits || !history.Truncated || history.TruncationReason != runtimeapi.GitHistoryLimit {
		t.Fatalf("history summary = commits:%d returned:%d truncated:%v reason:%q", len(history.Commits), history.ReturnedItems, history.Truncated, history.TruncationReason)
	}
	for _, commit := range history.Commits {
		if !fullGitHash.MatchString(commit.Hash) {
			t.Fatalf("non-full commit hash %q", commit.Hash)
		}
		if _, err := time.Parse(time.RFC3339, commit.Date); err != nil {
			t.Fatalf("non-RFC3339 date %q: %v", commit.Date, err)
		}
	}
}

func TestGitCommitFilesPaginationRenameAndHashValidation(t *testing.T) {
	root := initGitRepo(t)
	for i := 0; i < runtimeapi.PageDefaultItems+1; i++ {
		writeTestFile(t, root, fmt.Sprintf("files/file-%03d.txt", i), []byte("one\n"))
	}
	hash := commitAll(t, root, "many files")
	service := newTestService(t, root, nil, "")
	first, err := service.GitCommitDetail(context.Background(), runtimeapi.GitCommitDetailInput{Session: testSession, Hash: hash})
	if err != nil {
		t.Fatalf("GitCommitDetail files: %v", err)
	}
	if first.Kind != runtimeapi.GitDetailFiles || first.Files == nil || len(*first.Files) != runtimeapi.PageDefaultItems || first.HasMore == nil || !*first.HasMore || first.Next == "" {
		t.Fatalf("first commit page = %+v", first)
	}
	second, err := service.GitCommitDetail(context.Background(), runtimeapi.GitCommitDetailInput{Session: testSession, Hash: hash, Cursor: first.Next})
	if err != nil || second.Files == nil || len(*second.Files) != 1 || second.HasMore == nil || *second.HasMore {
		t.Fatalf("second commit page = %+v, err=%v", second, err)
	}
	_, err = service.GitCommitDetail(context.Background(), runtimeapi.GitCommitDetailInput{Session: testSession, Hash: "HEAD"})
	if !errors.Is(err, ErrGitObjectNotFound) {
		t.Fatalf("symbolic hash err = %v", err)
	}

	if err := os.Rename(filepath.Join(root, "files", "file-000.txt"), filepath.Join(root, "files", "renamed.txt")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	renameHash := commitAll(t, root, "rename")
	rename, err := service.GitCommitDetail(context.Background(), runtimeapi.GitCommitDetailInput{Session: testSession, Hash: renameHash})
	if err != nil || rename.Files == nil || len(*rename.Files) != 1 {
		t.Fatalf("rename detail = %+v, err=%v", rename, err)
	}
	file := (*rename.Files)[0]
	if file.Path != "files/renamed.txt" || file.OldPath != "files/file-000.txt" || !strings.HasPrefix(file.Status, "R") {
		t.Fatalf("rename file = %+v", file)
	}
}

func TestGitPatchCapUTF8AndUnavailableGit(t *testing.T) {
	root := initGitRepo(t)
	writeTestFile(t, root, "large.txt", nil)
	commitAll(t, root, "base")
	large := strings.Repeat("界", runtimeapi.GitPatchBytes/3+100000) + "\n"
	writeTestFile(t, root, "large.txt", []byte(large))
	hash := commitAll(t, root, "large patch")
	service := newTestService(t, root, nil, "")
	detail, err := service.GitCommitDetail(context.Background(), runtimeapi.GitCommitDetailInput{Session: testSession, Hash: hash, Path: "large.txt"})
	if err != nil {
		t.Fatalf("GitCommitDetail patch: %v", err)
	}
	if detail.Kind != runtimeapi.GitDetailPatch || detail.Body == nil || detail.SizeBytes == nil || detail.ReturnedBytes == nil || detail.Truncated == nil {
		t.Fatalf("patch shape = %+v", detail)
	}
	if !*detail.Truncated || detail.TruncationReason != runtimeapi.ByteLimit || *detail.SizeBytes <= *detail.ReturnedBytes || *detail.ReturnedBytes > runtimeapi.GitPatchBytes || !utf8.ValidString(*detail.Body) {
		t.Fatalf("patch bounds = size:%d returned:%d truncated:%v valid:%v", *detail.SizeBytes, *detail.ReturnedBytes, *detail.Truncated, utf8.ValidString(*detail.Body))
	}

	unavailable := newTestService(t, root, nil, filepath.Join(root, "missing-git"))
	_, err = unavailable.GitHistory(context.Background(), runtimeapi.GitHistoryInput{Session: testSession})
	if !errors.Is(err, ErrGitUnavailable) {
		t.Fatalf("unavailable Git history err = %v", err)
	}
}
