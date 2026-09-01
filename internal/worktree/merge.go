package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// MergeInspection describes the diff, commit divergence, and mergeability
// between a worktree branch and its base target branch.
type MergeInspection struct {
	Available      bool     `json:"available"`
	Reason         string   `json:"reason,omitempty"`
	WorktreeRoot   string   `json:"worktreeRoot,omitempty"`
	SourceRoot     string   `json:"sourceRoot,omitempty"`
	WorktreeBranch string   `json:"worktreeBranch,omitempty"`
	TargetBranch   string   `json:"targetBranch,omitempty"`
	AheadCount     int      `json:"aheadCount"`
	BehindCount    int      `json:"behindCount"`
	FilesChanged   int      `json:"filesChanged"`
	Insertions     int      `json:"insertions"`
	Deletions      int      `json:"deletions"`
	ChangedFiles   []string `json:"changedFiles,omitempty"`
	HasConflicts   bool     `json:"hasConflicts"`
	ConflictFiles  []string `json:"conflictFiles,omitempty"`
	WorktreeDirty  bool     `json:"worktreeDirty"`
	SourceDirty    bool     `json:"sourceDirty"`
}

// MergeOptions specifies configuration for merging a worktree branch back into
// the base repository.
type MergeOptions struct {
	AutoCommitDirty bool `json:"autoCommitDirty"`
	RemoveWorktree  bool `json:"removeWorktree"`
	DeleteBranch    bool `json:"deleteBranch"`
}

// MergeResult reports the outcome of merging a worktree back into the base.
type MergeResult struct {
	Merged          bool   `json:"merged"`
	TargetBranch    string `json:"targetBranch"`
	MergedCommit    string `json:"mergedCommit,omitempty"`
	WorktreeRemoved bool   `json:"worktreeRemoved"`
	BranchDeleted   bool   `json:"branchDeleted"`
	Error           string `json:"error,omitempty"`
}

// InspectMerge inspects the diff, commit divergence, and merge conflicts
// between the worktree at workspaceRoot and the primary repository branch.
func InspectMerge(ctx context.Context, workspaceRoot string) (MergeInspection, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return MergeInspection{Available: false, Reason: "workspace root is required"}, errors.New("workspace root is required")
	}

	roots, err := resolveWorktreeRoots(ctx, workspaceRoot)
	if err != nil {
		return MergeInspection{Available: false, Reason: err.Error()}, err
	}

	wtStatus, _, _ := runGit(ctx, roots.worktreeRoot, "status", "--porcelain=v1")
	worktreeDirty := strings.TrimSpace(wtStatus) != ""

	srcStatus, _, _ := runGit(ctx, roots.sourceRoot, "status", "--porcelain=v1")
	sourceDirty := strings.TrimSpace(srcStatus) != ""

	aheadCount, behindCount := queryAheadBehind(ctx, roots.worktreeRoot, roots.targetBranch)
	filesChanged, insertions, deletions, changedFiles := calculateDiffStats(ctx, roots.worktreeRoot, roots.targetBranch, worktreeDirty, wtStatus)
	hasConflicts, conflictFiles := checkMergeConflicts(ctx, roots.worktreeRoot, roots.targetBranch)

	return MergeInspection{
		Available:      true,
		WorktreeRoot:   roots.worktreeRoot,
		SourceRoot:     roots.sourceRoot,
		WorktreeBranch: roots.worktreeBranch,
		TargetBranch:   roots.targetBranch,
		AheadCount:     aheadCount,
		BehindCount:    behindCount,
		FilesChanged:   filesChanged,
		Insertions:     insertions,
		Deletions:      deletions,
		ChangedFiles:   changedFiles,
		HasConflicts:   hasConflicts,
		ConflictFiles:  conflictFiles,
		WorktreeDirty:  worktreeDirty,
		SourceDirty:    sourceDirty,
	}, nil
}

type worktreeRoots struct {
	worktreeRoot   string
	sourceRoot     string
	worktreeBranch string
	targetBranch   string
}

func resolveWorktreeRoots(ctx context.Context, workspaceRoot string) (worktreeRoots, error) {
	worktreeRoot, _, err := runGit(ctx, workspaceRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return worktreeRoots{}, fmt.Errorf("resolve worktree root: %w", err)
	}
	worktreeRoot = filepath.Clean(strings.TrimSpace(worktreeRoot))

	commonDir, _, err := runGit(ctx, worktreeRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return worktreeRoots{}, err
	}
	commonDir = strings.TrimSpace(commonDir)
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreeRoot, commonDir)
	}
	commonDir = filepath.Clean(commonDir)

	sourceRoot := filepath.Dir(commonDir)
	if filepath.Base(commonDir) == ".git" {
		sourceRoot = filepath.Dir(commonDir)
	}

	worktreeBranch, _, _ := runGit(ctx, worktreeRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	worktreeBranch = strings.TrimSpace(worktreeBranch)
	if worktreeBranch == "" {
		worktreeBranch, _, _ = runGit(ctx, worktreeRoot, "rev-parse", "--short", "HEAD")
		worktreeBranch = strings.TrimSpace(worktreeBranch)
	}

	targetBranch, _, _ := runGit(ctx, sourceRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	targetBranch = strings.TrimSpace(targetBranch)
	if targetBranch == "" {
		targetBranch = "main"
	}

	return worktreeRoots{
		worktreeRoot:   worktreeRoot,
		sourceRoot:     sourceRoot,
		worktreeBranch: worktreeBranch,
		targetBranch:   targetBranch,
	}, nil
}

func queryAheadBehind(ctx context.Context, worktreeRoot, targetBranch string) (ahead, behind int) {
	if out, _, err := runGit(ctx, worktreeRoot, "rev-list", "--count", targetBranch+"..HEAD"); err == nil {
		ahead, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	if out, _, err := runGit(ctx, worktreeRoot, "rev-list", "--count", "HEAD.."+targetBranch); err == nil {
		behind, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	return ahead, behind
}

func calculateDiffStats(ctx context.Context, worktreeRoot, targetBranch string, worktreeDirty bool, wtStatus string) (filesChanged, insertions, deletions int, changedFiles []string) {
	seen := make(map[string]bool)

	if out, _, err := runGit(ctx, worktreeRoot, "diff", "--name-only", targetBranch+"...HEAD"); err == nil {
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" && !seen[trimmed] {
				changedFiles = append(changedFiles, trimmed)
				seen[trimmed] = true
			}
		}
	}

	if worktreeDirty {
		for line := range strings.SplitSeq(wtStatus, "\n") {
			line = strings.TrimRight(line, "\r")
			if len(line) > 3 {
				filePath := strings.TrimSpace(line[3:])
				if strings.Contains(filePath, "->") {
					parts := strings.Split(filePath, "->")
					filePath = strings.TrimSpace(parts[len(parts)-1])
				}
				if filePath != "" && !seen[filePath] {
					changedFiles = append(changedFiles, filePath)
					seen[filePath] = true
				}
			}
		}
	}

	if out, _, err := runGit(ctx, worktreeRoot, "diff", "--shortstat", targetBranch+"...HEAD"); err == nil {
		for p := range strings.SplitSeq(out, ",") {
			p = strings.TrimSpace(p)
			if strings.Contains(p, "insertion") {
				fields := strings.Fields(p)
				if len(fields) > 0 {
					insertions, _ = strconv.Atoi(fields[0])
				}
			} else if strings.Contains(p, "deletion") {
				fields := strings.Fields(p)
				if len(fields) > 0 {
					deletions, _ = strconv.Atoi(fields[0])
				}
			}
		}
	}

	return len(changedFiles), insertions, deletions, changedFiles
}

func checkMergeConflicts(ctx context.Context, worktreeRoot, targetBranch string) (hasConflicts bool, conflictFiles []string) {
	mergeBase, _, _ := runGit(ctx, worktreeRoot, "merge-base", targetBranch, "HEAD")
	mergeBase = strings.TrimSpace(mergeBase)
	if mergeBase == "" {
		return false, nil
	}
	mtOut, _, _ := runGit(ctx, worktreeRoot, "merge-tree", mergeBase, targetBranch, "HEAD")
	if strings.Contains(mtOut, "changed in both") || strings.Contains(mtOut, "<<<<<<<") || strings.Contains(mtOut, "CONFLICT") {
		hasConflicts = true
		for line := range strings.SplitSeq(mtOut, "\n") {
			if strings.Contains(line, "changed in both") {
				parts := strings.Fields(line)
				if len(parts) > 0 {
					conflictFiles = append(conflictFiles, parts[len(parts)-1])
				}
			}
		}
	}
	return hasConflicts, conflictFiles
}

// MergeBack merges the worktree branch into the base repository branch and
// optionally cleans up the worktree and temporary branch.
func MergeBack(ctx context.Context, workspaceRoot string, opts MergeOptions) (MergeResult, error) {
	insp, err := InspectMerge(ctx, workspaceRoot)
	if err != nil {
		return MergeResult{Merged: false, Error: err.Error()}, err
	}
	if !insp.Available {
		return MergeResult{Merged: false, Error: insp.Reason}, errors.New(insp.Reason)
	}

	if insp.HasConflicts {
		return MergeResult{
			Merged:       false,
			TargetBranch: insp.TargetBranch,
			Error:        fmt.Sprintf("cannot auto-merge: %d conflict(s) detected with %s", len(insp.ConflictFiles), insp.TargetBranch),
		}, fmt.Errorf("merge conflict with %s", insp.TargetBranch)
	}

	if insp.WorktreeDirty {
		if !opts.AutoCommitDirty {
			return MergeResult{
				Merged:       false,
				TargetBranch: insp.TargetBranch,
				Error:        "worktree has uncommitted changes",
			}, errors.New("worktree has uncommitted changes")
		}
		_, _, _ = runGit(ctx, insp.WorktreeRoot, "add", "-A")
		_, stderr, commitErr := runGit(ctx, insp.WorktreeRoot, "-c", "user.name=Reasonix", "-c", "user.email=reasonix@local", "commit", "-m", "worktree: save changes before merge back")
		if commitErr != nil {
			return MergeResult{
				Merged:       false,
				TargetBranch: insp.TargetBranch,
				Error:        fmt.Sprintf("auto-commit failed: %s", stderr),
			}, fmt.Errorf("auto-commit dirty worktree: %w", commitErr)
		}
	}

	mergeMsg := fmt.Sprintf("Merge worktree branch '%s' into %s", insp.WorktreeBranch, insp.TargetBranch)
	_, stderr, mergeErr := runGit(ctx, insp.SourceRoot, "-c", "user.name=Reasonix", "-c", "user.email=reasonix@local", "merge", "--no-ff", "-m", mergeMsg, insp.WorktreeBranch)
	if mergeErr != nil {
		return MergeResult{
			Merged:       false,
			TargetBranch: insp.TargetBranch,
			Error:        fmt.Sprintf("merge failed: %s", stderr),
		}, fmt.Errorf("git merge: %w%s", mergeErr, stderrSuffix(stderr))
	}

	mergedHead, _, _ := runGit(ctx, insp.SourceRoot, "rev-parse", "HEAD")
	mergedHead = strings.TrimSpace(mergedHead)

	var worktreeRemoved bool
	var branchDeleted bool

	if opts.RemoveWorktree && insp.WorktreeRoot != "" {
		if _, _, remErr := runGit(ctx, insp.SourceRoot, "worktree", "remove", insp.WorktreeRoot, "--force"); remErr == nil {
			worktreeRemoved = true
			_ = os.RemoveAll(insp.WorktreeRoot)
		}
	}

	if opts.DeleteBranch && insp.WorktreeBranch != "" && insp.WorktreeBranch != insp.TargetBranch {
		if _, _, delErr := runGit(ctx, insp.SourceRoot, "branch", "-D", insp.WorktreeBranch); delErr == nil {
			branchDeleted = true
		}
	}

	return MergeResult{
		Merged:          true,
		TargetBranch:    insp.TargetBranch,
		MergedCommit:    mergedHead,
		WorktreeRemoved: worktreeRemoved,
		BranchDeleted:   branchDeleted,
	}, nil
}
