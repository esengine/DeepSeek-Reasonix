package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
	"reasonix/internal/worktree"
)

const topicWorktreesFile = "desktop-topic-worktrees.json"

var (
	inspectTopicWorktree = worktree.Inspect
	createTopicWorktree  = func(ctx context.Context, root, managed string) (worktree.Result, error) {
		return worktree.CreateKind(ctx, root, managed, worktree.BranchKindTopic)
	}
	removePristineTopicWorktree = worktree.RemovePristine
	topicWorktreeBindingsMu     sync.Mutex
)

// TopicWorktreeBinding records an opt-in per-topic Git worktree (#4304).
// Topic metadata and sessions stay keyed by SourceRoot; the agent boots in
// WorkspaceRoot so file edits stay isolated from the shared checkout.
type TopicWorktreeBinding struct {
	TopicID       string `json:"topicId"`
	SourceRoot    string `json:"sourceRoot"`
	WorkspaceRoot string `json:"workspaceRoot"`
	WorktreeRoot  string `json:"worktreeRoot"`
	Branch        string `json:"branch"`
	Head          string `json:"head"`
	CreatedAt     int64  `json:"createdAt"`
}

// TopicWorktreeOpenResult is returned after an isolated topic worktree has been
// created and opened under the source project.
type TopicWorktreeOpenResult struct {
	TopicID       string                `json:"topicId"`
	SourceRoot    string                `json:"sourceRoot"`
	WorkspaceRoot string                `json:"workspaceRoot"`
	WorktreeRoot  string                `json:"worktreeRoot"`
	Branch        string                `json:"branch"`
	SourceDirty   bool                  `json:"sourceDirty"`
	Tab           TabMeta               `json:"tab"`
	Availability  worktree.Availability `json:"availability,omitempty"`
}

// TopicWorktreeAvailability reports whether sourceRoot can use per-topic
// isolation. False never blocks ordinary shared-checkout topics.
func (a *App) TopicWorktreeAvailability(sourceRoot string) worktree.Availability {
	return inspectTopicWorktree(a.bootContext(), sourceRoot)
}

// CreateTopicWorktree creates a Topic under sourceRoot, binds it to a new
// managed worktree, and opens a tab whose runtime root is the worktree while
// the sidebar identity stays on the source project. Worktrees are never deleted
// by this call when later UI steps fail.
func (a *App) CreateTopicWorktree(sourceRoot string) (TopicWorktreeOpenResult, error) {
	sourceRoot = strings.TrimSpace(sourceRoot)
	if sourceRoot == "" {
		return TopicWorktreeOpenResult{}, fmt.Errorf("workspaceRoot is required")
	}
	if abs, err := filepath.Abs(sourceRoot); err == nil {
		sourceRoot = abs
	}
	avail := inspectTopicWorktree(a.bootContext(), sourceRoot)
	if !avail.Available {
		reason := strings.TrimSpace(avail.Reason)
		if reason == "" {
			reason = "Git worktree isolation is unavailable for this project"
		}
		return TopicWorktreeOpenResult{Availability: avail}, fmt.Errorf("%s", reason)
	}

	created, err := createTopicWorktree(a.bootContext(), sourceRoot, config.DeliveryWorktreeDir())
	if err != nil {
		return TopicWorktreeOpenResult{}, err
	}

	topic, err := a.CreateTopic("project", sourceRoot, "")
	if err != nil {
		return TopicWorktreeOpenResult{}, fmt.Errorf("isolated worktree was created at %s but Reasonix could not create a topic: %w", created.WorktreeRoot, err)
	}
	binding := TopicWorktreeBinding{
		TopicID:       topic.ID,
		SourceRoot:    sourceRoot,
		WorkspaceRoot: created.WorkspaceRoot,
		WorktreeRoot:  created.WorktreeRoot,
		Branch:        created.Branch,
		Head:          created.Head,
		CreatedAt:     time.Now().UnixMilli(),
	}
	if err := saveTopicWorktreeBinding(sourceRoot, binding); err != nil {
		return TopicWorktreeOpenResult{}, fmt.Errorf("isolated worktree was created at %s but Reasonix could not record the topic binding: %w", created.WorktreeRoot, err)
	}
	writeTopicWorktreeCreatedHead(created.WorktreeRoot, created.Head)

	var tab TabMeta
	if a.singleSurfaceLayoutEnabled() {
		tab, err = a.openTopicWorktreeSurface(sourceRoot, created.WorkspaceRoot, topic.ID)
	} else {
		tab, err = a.openTopicWorktreeTab(sourceRoot, created.WorkspaceRoot, topic.ID)
	}
	if err != nil {
		return TopicWorktreeOpenResult{}, fmt.Errorf("isolated worktree was created at %s but Reasonix could not open it: %w", created.WorktreeRoot, err)
	}
	return TopicWorktreeOpenResult{
		TopicID:       topic.ID,
		SourceRoot:    sourceRoot,
		WorkspaceRoot: created.WorkspaceRoot,
		WorktreeRoot:  created.WorktreeRoot,
		Branch:        created.Branch,
		SourceDirty:   created.SourceDirty,
		Tab:           tab,
		Availability:  avail,
	}, nil
}

func (a *App) openTopicWorktreeSurface(sourceRoot, runtimeRoot, topicID string) (TabMeta, error) {
	a.singleSurfaceMu.Lock()
	defer a.singleSurfaceMu.Unlock()
	meta, err := a.openTopicWorktreeTab(sourceRoot, runtimeRoot, topicID)
	if err != nil {
		return TabMeta{}, err
	}
	return a.keepOnlyVisibleTab(meta.ID)
}

func (a *App) openTopicWorktreeTab(sourceRoot, runtimeRoot, topicID string) (TabMeta, error) {
	// Register the source project only — the managed worktree must not appear
	// as a sibling project in the sidebar.
	saveWorkspace(sourceRoot)
	a.registerProjectRoot(sourceRoot)
	return a.openTopicTabWithRoots("project", sourceRoot, runtimeRoot, topicID, "", true)
}

// resolveTopicRuntimeRoot returns the worktree workspace for an opt-in topic
// when the binding still points at a live directory; otherwise the source root.
func resolveTopicRuntimeRoot(sourceRoot, topicID string) (runtimeRoot, source string) {
	sourceRoot = normalizeProjectRoot(sourceRoot)
	topicID = strings.TrimSpace(topicID)
	if sourceRoot == "" || topicID == "" {
		return sourceRoot, sourceRoot
	}
	binding, ok := loadTopicWorktreeBinding(sourceRoot, topicID)
	if !ok {
		return sourceRoot, sourceRoot
	}
	runtime := strings.TrimSpace(binding.WorkspaceRoot)
	if runtime == "" {
		return sourceRoot, sourceRoot
	}
	st, err := os.Stat(runtime)
	if err != nil || !st.IsDir() {
		return sourceRoot, sourceRoot
	}
	return runtime, sourceRoot
}

func tabLogicalProjectRoot(tab *WorkspaceTab) string {
	if tab == nil {
		return ""
	}
	if root := strings.TrimSpace(tab.SourceRoot); root != "" {
		return root
	}
	return tab.WorkspaceRoot
}

func topicWorktreesPath(sourceRoot string) string {
	sourceRoot = strings.TrimSpace(sourceRoot)
	if sourceRoot == "" {
		return ""
	}
	return filepath.Join(sourceRoot, ".reasonix", topicWorktreesFile)
}

func loadTopicWorktreeBinding(sourceRoot, topicID string) (TopicWorktreeBinding, bool) {
	all := loadTopicWorktreeBindings(sourceRoot)
	binding, ok := all[strings.TrimSpace(topicID)]
	return binding, ok
}

func loadTopicWorktreeBindings(sourceRoot string) map[string]TopicWorktreeBinding {
	path := topicWorktreesPath(sourceRoot)
	if path == "" {
		return map[string]TopicWorktreeBinding{}
	}
	topicWorktreeBindingsMu.Lock()
	defer topicWorktreeBindingsMu.Unlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return map[string]TopicWorktreeBinding{}
	}
	var file struct {
		Bindings []TopicWorktreeBinding `json:"bindings"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return map[string]TopicWorktreeBinding{}
	}
	out := make(map[string]TopicWorktreeBinding, len(file.Bindings))
	for _, binding := range file.Bindings {
		id := strings.TrimSpace(binding.TopicID)
		if id == "" || strings.TrimSpace(binding.WorkspaceRoot) == "" {
			continue
		}
		binding.TopicID = id
		binding.SourceRoot = normalizeProjectRoot(binding.SourceRoot)
		if binding.SourceRoot == "" {
			binding.SourceRoot = normalizeProjectRoot(sourceRoot)
		}
		out[id] = binding
	}
	return out
}

func saveTopicWorktreeBinding(sourceRoot string, binding TopicWorktreeBinding) error {
	sourceRoot = normalizeProjectRoot(sourceRoot)
	binding.TopicID = strings.TrimSpace(binding.TopicID)
	binding.SourceRoot = normalizeProjectRoot(binding.SourceRoot)
	if binding.SourceRoot == "" {
		binding.SourceRoot = sourceRoot
	}
	binding.WorkspaceRoot = strings.TrimSpace(binding.WorkspaceRoot)
	binding.WorktreeRoot = strings.TrimSpace(binding.WorktreeRoot)
	if sourceRoot == "" || binding.TopicID == "" || binding.WorkspaceRoot == "" {
		return fmt.Errorf("topic worktree binding is incomplete")
	}
	path := topicWorktreesPath(sourceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	topicWorktreeBindingsMu.Lock()
	defer topicWorktreeBindingsMu.Unlock()

	existing := map[string]TopicWorktreeBinding{}
	if raw, err := os.ReadFile(path); err == nil {
		var file struct {
			Bindings []TopicWorktreeBinding `json:"bindings"`
		}
		if json.Unmarshal(raw, &file) == nil {
			for _, item := range file.Bindings {
				id := strings.TrimSpace(item.TopicID)
				if id == "" {
					continue
				}
				existing[id] = item
			}
		}
	}
	existing[binding.TopicID] = binding
	list := make([]TopicWorktreeBinding, 0, len(existing))
	for _, item := range existing {
		list = append(list, item)
	}
	payload, err := json.MarshalIndent(struct {
		Bindings []TopicWorktreeBinding `json:"bindings"`
	}{Bindings: list}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func topicHasWorktreeBinding(sourceRoot, topicID string) bool {
	_, ok := loadTopicWorktreeBinding(sourceRoot, topicID)
	return ok
}

func removeTopicWorktreeBinding(sourceRoot, topicID string) error {
	sourceRoot = normalizeProjectRoot(sourceRoot)
	topicID = strings.TrimSpace(topicID)
	if sourceRoot == "" || topicID == "" {
		return nil
	}
	path := topicWorktreesPath(sourceRoot)
	topicWorktreeBindingsMu.Lock()
	defer topicWorktreeBindingsMu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file struct {
		Bindings []TopicWorktreeBinding `json:"bindings"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return nil
	}
	next := make([]TopicWorktreeBinding, 0, len(file.Bindings))
	removed := false
	for _, item := range file.Bindings {
		if strings.TrimSpace(item.TopicID) == topicID {
			removed = true
			// Persist creation head into the worktree before dropping the
			// binding so orphan reclaim can still distinguish pristine trees.
			if head := strings.TrimSpace(item.Head); head != "" {
				root := strings.TrimSpace(item.WorktreeRoot)
				if root == "" {
					root = strings.TrimSpace(item.WorkspaceRoot)
				}
				if root != "" && readTopicWorktreeCreatedHead(root) == "" {
					writeTopicWorktreeCreatedHead(root, head)
				}
			}
			continue
		}
		next = append(next, item)
	}
	if !removed {
		return nil
	}
	payload, err := json.MarshalIndent(struct {
		Bindings []TopicWorktreeBinding `json:"bindings"`
	}{Bindings: next}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func sourceRootForManagedTopicWorktree(managedRoot, topicID string) string {
	managedRoot = filepath.Clean(strings.TrimSpace(managedRoot))
	topicID = strings.TrimSpace(topicID)
	if managedRoot == "" {
		return ""
	}
	f := loadProjectsFile()
	for _, project := range f.Projects {
		bindings := loadTopicWorktreeBindings(project.Root)
		if topicID != "" {
			if binding, ok := bindings[topicID]; ok {
				if sameProjectRoot(binding.WorkspaceRoot, managedRoot) || sameProjectRoot(binding.WorktreeRoot, managedRoot) {
					return normalizeProjectRoot(project.Root)
				}
			}
			continue
		}
		for _, binding := range bindings {
			if sameProjectRoot(binding.WorkspaceRoot, managedRoot) || sameProjectRoot(binding.WorktreeRoot, managedRoot) {
				return normalizeProjectRoot(project.Root)
			}
		}
	}
	return ""
}

const topicWorktreeCreatedHeadFile = ".reasonix-topic-created-head"

func writeTopicWorktreeCreatedHead(worktreeRoot, head string) {
	worktreeRoot = strings.TrimSpace(worktreeRoot)
	head = strings.TrimSpace(head)
	if worktreeRoot == "" || head == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(worktreeRoot, topicWorktreeCreatedHeadFile), []byte(head+"\n"), 0o644)
}

func readTopicWorktreeCreatedHead(worktreeRoot string) string {
	raw, err := os.ReadFile(filepath.Join(worktreeRoot, topicWorktreeCreatedHeadFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// reclaimOrphanTopicWorktrees removes managed topic worktrees that are no
// longer referenced by any project binding and remain pristine. Dirty or
// advanced worktrees are skipped. Without a durable created-head marker,
// unbound trees are left alone so committed work cannot be force-removed.
func (a *App) reclaimOrphanTopicWorktrees() {
	managed := strings.TrimSpace(config.DeliveryWorktreeDir())
	if managed == "" {
		return
	}
	bound := boundTopicWorktreeRoots()
	_ = filepath.WalkDir(managed, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(managed, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 {
			return nil
		}
		clean := filepath.Clean(path)
		if bound[clean] {
			return filepath.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr != nil {
			return nil
		}
		branchOut, _, branchErr := worktree.RunGit(a.bootContext(), path, "symbolic-ref", "--quiet", "--short", "HEAD")
		if branchErr != nil || !strings.HasPrefix(strings.TrimSpace(branchOut), "reasonix/topic-") {
			return filepath.SkipDir
		}
		createdHead := readTopicWorktreeCreatedHead(path)
		if createdHead == "" {
			return filepath.SkipDir
		}
		common, _, sourceErr := worktree.RunGit(a.bootContext(), path, "rev-parse", "--git-common-dir")
		if sourceErr != nil {
			return filepath.SkipDir
		}
		common = strings.TrimSpace(common)
		if !filepath.IsAbs(common) {
			common = filepath.Join(path, common)
		}
		common = filepath.Clean(common)
		source := common
		if filepath.Base(source) == ".git" {
			source = filepath.Dir(source)
		}
		_ = removePristineTopicWorktree(a.bootContext(), source, path, createdHead)
		return filepath.SkipDir
	})
}

func boundTopicWorktreeRoots() map[string]bool {
	out := map[string]bool{}
	f := loadProjectsFile()
	for _, project := range f.Projects {
		for _, binding := range loadTopicWorktreeBindings(project.Root) {
			if root := strings.TrimSpace(binding.WorktreeRoot); root != "" {
				out[filepath.Clean(root)] = true
			}
			if root := strings.TrimSpace(binding.WorkspaceRoot); root != "" {
				out[filepath.Clean(root)] = true
			}
		}
	}
	return out
}
