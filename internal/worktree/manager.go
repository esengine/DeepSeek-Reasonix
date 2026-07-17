package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const resourceMetadataFile = ".reasonix-worktree.json"

// Kind identifies the feature that owns a managed worktree resource.
type Kind string

const (
	KindDelivery Kind = "delivery"
	KindSubagent Kind = "subagent"
)

// LifecycleState is persisted so crashed or resumed sessions can recover the
// state of a managed worktree without guessing from UI history.
type LifecycleState string

const (
	LifecycleStateCreated LifecycleState = "created"
	LifecycleStateApplied LifecycleState = "applied"
	LifecycleStateRemoved LifecycleState = "removed"
)

// CleanupState describes whether the resource is still present on disk.
type CleanupState string

const (
	CleanupStateActive  CleanupState = "active"
	CleanupStateOrphan  CleanupState = "orphaned"
	CleanupStateRemoved CleanupState = "removed"
)

// DirtyPolicy controls how a source workspace with uncommitted changes is
// handled before a child worktree is created.
type DirtyPolicy string

const (
	DirtyPolicyCommittedHead DirtyPolicy = "committed-head"
	DirtyPolicyReject        DirtyPolicy = "reject"
)

var (
	ErrDirtySource        = errors.New("source workspace has uncommitted changes")
	ErrApplyConflict      = errors.New("worktree apply conflict")
	ErrResourceHasChanges = errors.New("worktree resource has changes")
)

// DirtySourceError is returned when policy requires a clean parent workspace.
type DirtySourceError struct {
	WorkspaceRoot string
}

func (e *DirtySourceError) Error() string {
	if strings.TrimSpace(e.WorkspaceRoot) == "" {
		return ErrDirtySource.Error()
	}
	return fmt.Sprintf("%s: %s", ErrDirtySource, e.WorkspaceRoot)
}

func (e *DirtySourceError) Unwrap() error { return ErrDirtySource }

// CreatePolicy is the stable policy surface for managed worktree creation.
type CreatePolicy struct {
	Kind         Kind
	BranchPrefix string
	DirtyPolicy  DirtyPolicy
	Durable      bool
}

// Resource is the durable identity of one managed worktree.
type Resource struct {
	IsolationID    string         `json:"isolation_id"`
	Kind           Kind           `json:"kind"`
	WorkspaceRoot  string         `json:"workspace_root"`
	WorktreeRoot   string         `json:"worktree_root"`
	SourceRoot     string         `json:"source_root"`
	Branch         string         `json:"branch"`
	BaseCommit     string         `json:"base_commit"`
	HeadCommit     string         `json:"head_commit"`
	SourceDirty    bool           `json:"source_dirty"`
	LifecycleState LifecycleState `json:"lifecycle_state"`
	CleanupState   CleanupState   `json:"cleanup_state"`
	CreatedAt      time.Time      `json:"created_at"`

	metadataPath string
}

// Status reports the current diff surface without mutating either workspace.
type Status struct {
	Dirty   bool     `json:"dirty"`
	Paths   []string `json:"paths,omitempty"`
	RawText string   `json:"raw_text,omitempty"`
}

// Diff contains the patch text and path list for explicit review/apply.
type Diff struct {
	Patch string   `json:"patch,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

// ApplyStatus is a structured apply result, not just a human string.
type ApplyStatus string

const (
	ApplyStatusClean      ApplyStatus = "clean"
	ApplyStatusApplied    ApplyStatus = "applied"
	ApplyStatusConflicted ApplyStatus = "conflicted"
	ApplyStatusBlocked    ApplyStatus = "blocked"
)

// ApplyResult describes the outcome of applying a child worktree patch back to
// its source repository.
type ApplyResult struct {
	Status          ApplyStatus `json:"status"`
	Paths           []string    `json:"paths,omitempty"`
	ConflictedPaths []string    `json:"conflicted_paths,omitempty"`
	Message         string      `json:"message,omitempty"`
}

// ApplyConflictError carries a structured conflict result.
type ApplyConflictError struct {
	Result ApplyResult
}

func (e *ApplyConflictError) Error() string {
	if len(e.Result.ConflictedPaths) == 0 {
		return ErrApplyConflict.Error()
	}
	return fmt.Sprintf("%s: %s", ErrApplyConflict, strings.Join(e.Result.ConflictedPaths, ", "))
}

func (e *ApplyConflictError) Unwrap() error { return ErrApplyConflict }

// ResourceChangesError prevents implicit cleanup from deleting uncommitted
// files or commits that may belong to the user.
type ResourceChangesError struct {
	Paths       []string
	HeadChanged bool
}

func (e *ResourceChangesError) Error() string {
	parts := make([]string, 0, 2)
	if len(e.Paths) > 0 {
		parts = append(parts, "paths: "+strings.Join(e.Paths, ", "))
	}
	if e.HeadChanged {
		parts = append(parts, "branch contains commits after the isolation base")
	}
	if len(parts) == 0 {
		return ErrResourceHasChanges.Error()
	}
	return fmt.Sprintf("%s: %s", ErrResourceHasChanges, strings.Join(parts, "; "))
}

func (e *ResourceChangesError) Unwrap() error { return ErrResourceHasChanges }

// GCResult reports which orphaned resources were reclaimed and which were
// retained because their branch contains commits not represented elsewhere.
type GCResult struct {
	Removed []Resource `json:"removed,omitempty"`
	Blocked []Resource `json:"blocked,omitempty"`
}

// Manager owns the durable lifecycle for worktrees under one managed root.
type Manager struct {
	managedRoot string
}

// NewManager creates a worktree manager rooted at managedRoot.
func NewManager(managedRoot string) *Manager {
	return &Manager{managedRoot: strings.TrimSpace(managedRoot)}
}

func (m *Manager) managedRootOrError() (string, error) {
	if m == nil || strings.TrimSpace(m.managedRoot) == "" {
		return "", errors.New("Reasonix worktree storage is unavailable")
	}
	return m.managedRoot, nil
}

// Inspect checks whether workspaceRoot can be used as a source worktree.
func (m *Manager) Inspect(ctx context.Context, workspaceRoot string) Availability {
	return Inspect(ctx, workspaceRoot)
}

// Create makes a managed worktree resource according to policy.
func (m *Manager) Create(ctx context.Context, workspaceRoot string, policy CreatePolicy) (Resource, error) {
	managedRoot, err := m.managedRootOrError()
	if err != nil {
		return Resource{}, err
	}
	policy = normalizeCreatePolicy(policy)
	info, err := inspect(ctx, workspaceRoot)
	if err != nil {
		return Resource{}, err
	}
	if info.SourceDirty && policy.DirtyPolicy == DirtyPolicyReject {
		return Resource{}, &DirtySourceError{WorkspaceRoot: info.RepoRoot}
	}
	if err := os.MkdirAll(managedRoot, 0o700); err != nil {
		return Resource{}, fmt.Errorf("create Reasonix worktree storage: %w", err)
	}

	repoKey := repoStorageKey(info.commonDir)
	repoBase := safePathComponent(filepath.Base(info.RepoRoot))
	if repoBase == "" {
		repoBase = "repository"
	}

	now := time.Now().UTC()
	for attempt := 0; attempt < 5; attempt++ {
		id, randomErr := randomID()
		if randomErr != nil {
			return Resource{}, randomErr
		}
		branch := fmt.Sprintf("%s-%s-%s", strings.TrimRight(policy.BranchPrefix, "-/"), now.Format("20060102-150405"), id)
		worktreeRoot := filepath.Join(managedRoot, repoKey, id, repoBase)
		if _, statErr := os.Stat(worktreeRoot); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			return Resource{}, fmt.Errorf("inspect worktree destination: %w", statErr)
		}
		if err := os.MkdirAll(filepath.Dir(worktreeRoot), 0o700); err != nil {
			return Resource{}, fmt.Errorf("create worktree parent: %w", err)
		}

		_, stderr, addErr := runGit(ctx, info.RepoRoot, "worktree", "add", "-b", branch, worktreeRoot, info.head)
		if addErr != nil {
			if strings.Contains(strings.ToLower(stderr), "already exists") {
				continue
			}
			return Resource{}, fmt.Errorf("create Git worktree: %w%s", addErr, stderrSuffix(stderr))
		}

		selectedRoot := worktreeRoot
		if prefix := filepath.FromSlash(strings.Trim(strings.TrimSpace(info.prefix), "/")); prefix != "" && prefix != "." {
			selectedRoot = filepath.Join(worktreeRoot, prefix)
			st, statErr := os.Stat(selectedRoot)
			if statErr != nil || !st.IsDir() {
				return Resource{}, fmt.Errorf("created worktree is missing selected project subdirectory %q", prefix)
			}
		}
		res := Resource{
			IsolationID:    id,
			Kind:           policy.Kind,
			WorkspaceRoot:  selectedRoot,
			WorktreeRoot:   worktreeRoot,
			SourceRoot:     info.RepoRoot,
			Branch:         branch,
			BaseCommit:     info.head,
			HeadCommit:     info.head,
			SourceDirty:    info.SourceDirty,
			LifecycleState: LifecycleStateCreated,
			CleanupState:   CleanupStateActive,
			CreatedAt:      now,
			metadataPath:   resourceMetadataPath(managedRoot, repoKey, id),
		}
		if err := m.writeResource(res); err != nil {
			return Resource{}, err
		}
		return res, nil
	}
	return Resource{}, fmt.Errorf("could not allocate a unique %s worktree", policy.Kind)
}

// Show returns a resource by id.
func (m *Manager) Show(ctx context.Context, isolationID string) (Resource, error) {
	isolationID = strings.TrimSpace(isolationID)
	if isolationID == "" {
		return Resource{}, errors.New("worktree isolation id is required")
	}
	resources, err := m.List(ctx)
	if err != nil {
		return Resource{}, err
	}
	for _, res := range resources {
		if res.IsolationID == isolationID {
			return res, nil
		}
	}
	return Resource{}, fmt.Errorf("worktree isolation %q was not found", isolationID)
}

// List returns persisted worktree resources, including removed records.
func (m *Manager) List(ctx context.Context) ([]Resource, error) {
	managedRoot, err := m.managedRootOrError()
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	var resources []Resource
	if _, err := os.Stat(managedRoot); os.IsNotExist(err) {
		return resources, nil
	} else if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(managedRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != resourceMetadataFile {
			return nil
		}
		res, readErr := readResource(path)
		if readErr != nil {
			return readErr
		}
		if res.CleanupState == CleanupStateActive {
			if _, statErr := os.Stat(res.WorktreeRoot); os.IsNotExist(statErr) {
				res.CleanupState = CleanupStateOrphan
			}
		}
		res.metadataPath = path
		resources = append(resources, res)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].CreatedAt.Equal(resources[j].CreatedAt) {
			return resources[i].IsolationID < resources[j].IsolationID
		}
		return resources[i].CreatedAt.Before(resources[j].CreatedAt)
	})
	return resources, nil
}

// Status returns a porcelain status for the child worktree.
func (m *Manager) Status(ctx context.Context, res Resource) (Status, error) {
	if strings.TrimSpace(res.WorktreeRoot) == "" {
		return Status{}, errors.New("worktree root is required")
	}
	out, stderr, err := runGit(ctx, res.WorktreeRoot, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return Status{}, fmt.Errorf("inspect worktree status: %w%s", err, stderrSuffix(stderr))
	}
	paths := parsePorcelainPaths(out)
	return Status{Dirty: strings.TrimSpace(out) != "", Paths: paths, RawText: strings.TrimRight(out, "\n")}, nil
}

// Diff returns the tracked patch plus changed paths. Untracked files are listed
// in Paths but are deliberately not in Patch.
func (m *Manager) Diff(ctx context.Context, res Resource) (Diff, error) {
	status, err := m.Status(ctx, res)
	if err != nil {
		return Diff{}, err
	}
	patch, stderr, err := runGit(ctx, res.WorktreeRoot, "diff", "--binary", "HEAD")
	if err != nil {
		return Diff{}, fmt.Errorf("build worktree diff: %w%s", err, stderrSuffix(stderr))
	}
	return Diff{Patch: patch, Paths: status.Paths}, nil
}

// Apply explicitly applies a clean child worktree patch back to the source repo.
func (m *Manager) Apply(ctx context.Context, res Resource) (ApplyResult, error) {
	status, err := m.Status(ctx, res)
	if err != nil {
		return ApplyResult{}, err
	}
	if !status.Dirty {
		return ApplyResult{Status: ApplyStatusClean}, nil
	}
	untracked := untrackedPaths(status.RawText)
	if len(untracked) > 0 {
		return ApplyResult{Status: ApplyStatusBlocked, Paths: status.Paths, Message: "untracked files must be handled before apply"}, errors.New("cannot apply worktree with untracked files")
	}
	diff, err := m.Diff(ctx, res)
	if err != nil {
		return ApplyResult{}, err
	}
	if strings.TrimSpace(diff.Patch) == "" {
		return ApplyResult{Status: ApplyStatusBlocked, Paths: status.Paths, Message: "no tracked patch is available"}, errors.New("no tracked patch is available")
	}
	if _, stderr, checkErr := runGitInput(ctx, res.SourceRoot, []byte(diff.Patch), "apply", "--check", "-"); checkErr != nil {
		result := ApplyResult{
			Status:          ApplyStatusConflicted,
			Paths:           diff.Paths,
			ConflictedPaths: diff.Paths,
			Message:         strings.TrimSpace(stderr),
		}
		return result, &ApplyConflictError{Result: result}
	}
	if _, stderr, applyErr := runGitInput(ctx, res.SourceRoot, []byte(diff.Patch), "apply", "-"); applyErr != nil {
		return ApplyResult{}, fmt.Errorf("apply worktree diff: %w%s", applyErr, stderrSuffix(stderr))
	}
	res.LifecycleState = LifecycleStateApplied
	if err := m.writeResource(res); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{Status: ApplyStatusApplied, Paths: diff.Paths}, nil
}

// Remove safely removes an unchanged worktree. It refuses both uncommitted
// changes and commits created after the isolation base.
func (m *Manager) Remove(ctx context.Context, res Resource) (Resource, error) {
	if res.CleanupState == CleanupStateRemoved {
		return res, nil
	}
	status, err := m.Status(ctx, res)
	if err != nil {
		return Resource{}, err
	}
	head, err := worktreeHead(ctx, res.WorktreeRoot)
	if err != nil {
		return Resource{}, err
	}
	if status.Dirty || head != res.BaseCommit {
		return Resource{}, &ResourceChangesError{Paths: status.Paths, HeadChanged: head != res.BaseCommit}
	}
	return m.removeResource(ctx, res, false)
}

// Discard explicitly removes a worktree and its managed branch even when it
// contains changes. Callers must put this behind their approval boundary.
func (m *Manager) Discard(ctx context.Context, res Resource) (Resource, error) {
	return m.removeResource(ctx, res, true)
}

// GC reclaims orphaned resources only when their managed branch still points
// at the original base commit. Branches with later commits are retained.
func (m *Manager) GC(ctx context.Context) (GCResult, error) {
	resources, err := m.List(ctx)
	if err != nil {
		return GCResult{}, err
	}
	var result GCResult
	for _, res := range resources {
		if res.CleanupState != CleanupStateOrphan {
			continue
		}
		head, exists, err := branchHead(ctx, res.SourceRoot, res.Branch)
		if err != nil {
			return result, err
		}
		if exists && head != res.BaseCommit {
			result.Blocked = append(result.Blocked, res)
			continue
		}
		if exists {
			if _, stderr, err := runGit(ctx, res.SourceRoot, "branch", "-D", res.Branch); err != nil {
				return result, fmt.Errorf("delete orphaned worktree branch: %w%s", err, stderrSuffix(stderr))
			}
		}
		res.LifecycleState = LifecycleStateRemoved
		res.CleanupState = CleanupStateRemoved
		if err := m.writeResource(res); err != nil {
			return result, err
		}
		result.Removed = append(result.Removed, res)
	}
	return result, nil
}

func (m *Manager) removeResource(ctx context.Context, res Resource, force bool) (Resource, error) {
	if res.CleanupState == CleanupStateRemoved {
		return res, nil
	}
	if _, err := os.Stat(res.WorktreeRoot); err == nil {
		args := []string{"worktree", "remove"}
		if force {
			args = append(args, "--force")
		}
		args = append(args, res.WorktreeRoot)
		if _, stderr, err := runGit(ctx, res.SourceRoot, args...); err != nil {
			return Resource{}, fmt.Errorf("remove managed worktree: %w%s", err, stderrSuffix(stderr))
		}
	} else if !os.IsNotExist(err) {
		return Resource{}, fmt.Errorf("inspect managed worktree: %w", err)
	}
	if _, exists, err := branchHead(ctx, res.SourceRoot, res.Branch); err != nil {
		return Resource{}, err
	} else if exists {
		if _, stderr, err := runGit(ctx, res.SourceRoot, "branch", "-D", res.Branch); err != nil {
			return Resource{}, fmt.Errorf("delete managed worktree branch: %w%s", err, stderrSuffix(stderr))
		}
	}
	res.LifecycleState = LifecycleStateRemoved
	res.CleanupState = CleanupStateRemoved
	if err := m.writeResource(res); err != nil {
		return Resource{}, err
	}
	return res, nil
}

func worktreeHead(ctx context.Context, root string) (string, error) {
	head, stderr, err := runGit(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve worktree head: %w%s", err, stderrSuffix(stderr))
	}
	return strings.TrimSpace(head), nil
}

func branchHead(ctx context.Context, sourceRoot, branch string) (head string, exists bool, err error) {
	head, _, err = runGit(ctx, sourceRoot, "rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return "", false, nil
	}
	return strings.TrimSpace(head), true, nil
}

func (m *Manager) writeResource(res Resource) error {
	path := res.metadataPath
	if path == "" {
		managedRoot, err := m.managedRootOrError()
		if err != nil {
			return err
		}
		repoKey := repoStorageKey(res.SourceRoot)
		path = resourceMetadataPath(managedRoot, repoKey, res.IsolationID)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create worktree metadata directory: %w", err)
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("encode worktree metadata: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write worktree metadata: %w", err)
	}
	return nil
}

func normalizeCreatePolicy(policy CreatePolicy) CreatePolicy {
	if policy.Kind == "" {
		policy.Kind = KindDelivery
	}
	if policy.BranchPrefix == "" {
		switch policy.Kind {
		case KindSubagent:
			policy.BranchPrefix = "reasonix/subagent"
		default:
			policy.BranchPrefix = "reasonix/delivery"
		}
	}
	if policy.DirtyPolicy == "" {
		switch policy.Kind {
		case KindSubagent:
			policy.DirtyPolicy = DirtyPolicyReject
		default:
			policy.DirtyPolicy = DirtyPolicyCommittedHead
		}
	}
	return policy
}

func repoStorageKey(commonDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(strings.TrimSpace(commonDir))))
	return fmt.Sprintf("%x", sum[:8])
}

func readResource(path string) (Resource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Resource{}, err
	}
	var res Resource
	if err := json.Unmarshal(data, &res); err != nil {
		return Resource{}, fmt.Errorf("decode worktree metadata %s: %w", path, err)
	}
	res.metadataPath = path
	return res, nil
}

func resourceMetadataPath(managedRoot, repoKey, id string) string {
	return filepath.Join(managedRoot, repoKey, id, resourceMetadataFile)
}

func parsePorcelainPaths(out string) []string {
	var paths []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		path := line
		if len(line) > 3 {
			path = line[3:]
		}
		path = strings.TrimSpace(path)
		if before, after, ok := strings.Cut(path, " -> "); ok {
			_ = before
			path = strings.TrimSpace(after)
		}
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func untrackedPaths(porcelain string) []string {
	var paths []string
	for _, line := range strings.Split(strings.TrimRight(porcelain, "\n"), "\n") {
		if strings.HasPrefix(line, "?? ") {
			paths = append(paths, strings.TrimSpace(line[3:]))
		}
	}
	return paths
}
