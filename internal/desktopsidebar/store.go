// Package desktopsidebar reads/writes the same desktop project-tree metadata
// files used by the Wails app (desktop-projects.json + topic title maps), so
// Electron multi-tab and Wails share sidebar state.
package desktopsidebar

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

const (
	desktopProjectsFile = "desktop-projects.json"
	topicTitlesFile     = "desktop-topic-titles.json"
	defaultTopicTitle   = "新的会话"
)

// Project is one workspace entry in desktop-projects.json.
type Project struct {
	Root         string   `json:"root"`
	Title        string   `json:"title,omitempty"`
	Color        string   `json:"color,omitempty"`
	Topics       []string `json:"topics"`
	PinnedTopics []string `json:"pinnedTopics,omitempty"`
}

// File is the on-disk desktop-projects.json document.
type File struct {
	GlobalTitle        string    `json:"globalTitle,omitempty"`
	GlobalColor        string    `json:"globalColor,omitempty"`
	GlobalTopics       []string  `json:"globalTopics,omitempty"`
	GlobalPinnedTopics []string  `json:"globalPinnedTopics,omitempty"`
	DeletedTopics      []string  `json:"deletedTopics,omitempty"`
	PinnedProjects     []string  `json:"pinnedProjects,omitempty"`
	SidebarOrder       []string  `json:"sidebarOrder,omitempty"`
	Projects           []Project `json:"projects"`
}

// TopicMeta is returned by CreateTopic.
type TopicMeta struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
}

// Node is a ProjectTree node compatible with desktop.frontend ProjectNode.
type Node struct {
	Key            string  `json:"key"`
	Kind           string  `json:"kind"`
	Label          string  `json:"label"`
	Root           string  `json:"root,omitempty"`
	TopicID        string  `json:"topicId,omitempty"`
	SessionPath    string  `json:"sessionPath,omitempty"`
	ProjectColor   string  `json:"projectColor,omitempty"`
	Turns          int     `json:"turns,omitempty"`
	CreatedAt      int64   `json:"createdAt,omitempty"`
	LastActivityAt int64   `json:"lastActivityAt,omitempty"`
	Open           bool    `json:"open,omitempty"`
	Running        bool    `json:"running,omitempty"`
	Pinned         bool    `json:"pinned,omitempty"`
	Children       []Node  `json:"children,omitempty"`
}

// SessionHint is a lightweight session row used when building the tree.
type SessionHint struct {
	Path           string
	WorkspaceRoot  string
	TopicID        string
	TopicTitle     string
	Turns          int
	LastActivityAt int64
	Running        bool
}

var mu sync.Mutex

func configDir() string { return config.ReasonixHomeDir() }

func projectsPath() string {
	return filepath.Join(configDir(), desktopProjectsFile)
}

func topicTitlesPath(workspaceRoot string) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return filepath.Join(configDir(), "global", topicTitlesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicTitlesFile)
}

func normalizeRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

func sameRoot(a, b string) bool {
	return filepath.Clean(normalizeRoot(a)) == filepath.Clean(normalizeRoot(b))
}

// LoadProjects reads desktop-projects.json (empty file on missing).
func LoadProjects() File {
	mu.Lock()
	defer mu.Unlock()
	return loadProjectsLocked()
}

func loadProjectsLocked() File {
	b, err := os.ReadFile(projectsPath())
	if err != nil {
		return File{}
	}
	var f File
	_ = json.Unmarshal(b, &f)
	return f
}

func saveProjectsLocked(f File) error {
	if err := os.MkdirAll(configDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	path := projectsPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func updateProjects(mutator func(*File) (bool, error)) error {
	mu.Lock()
	defer mu.Unlock()
	f := loadProjectsLocked()
	changed, err := mutator(&f)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return saveProjectsLocked(f)
}

func loadTopicTitles(workspaceRoot string) map[string]string {
	b, err := os.ReadFile(topicTitlesPath(workspaceRoot))
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]string{}
	}
	return m
}

func saveTopicTitles(workspaceRoot string, m map[string]string) error {
	path := topicTitlesPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmp, path)
}

func newTopicID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "topic_" + hex.EncodeToString(b[:])
}

// EnsureProject registers workspaceRoot in desktop-projects.json if missing.
func EnsureProject(workspaceRoot string) error {
	root := normalizeRoot(workspaceRoot)
	if root == "" {
		return fmt.Errorf("workspaceRoot required")
	}
	return updateProjects(func(f *File) (bool, error) {
		for _, p := range f.Projects {
			if sameRoot(p.Root, root) {
				return false, nil
			}
		}
		f.Projects = append(f.Projects, Project{Root: root, Topics: []string{"main"}})
		return true, nil
	})
}

// CreateTopic creates a topic id under a project (or global) and returns metadata.
func CreateTopic(scope, workspaceRoot, title string) (TopicMeta, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = defaultTopicTitle
	}
	root := ""
	if scope != "global" {
		root = normalizeRoot(workspaceRoot)
		if root == "" {
			return TopicMeta{}, fmt.Errorf("workspaceRoot required for project topic")
		}
		if err := EnsureProject(root); err != nil {
			return TopicMeta{}, err
		}
	}
	id := newTopicID()
	createdAt := time.Now().UnixMilli()
	titles := loadTopicTitles(root)
	titles[id] = title
	if err := saveTopicTitles(root, titles); err != nil {
		return TopicMeta{}, err
	}
	err := updateProjects(func(f *File) (bool, error) {
		if root == "" {
			f.GlobalTopics = prependUnique(f.GlobalTopics, id)
			return true, nil
		}
		for i := range f.Projects {
			if sameRoot(f.Projects[i].Root, root) {
				f.Projects[i].Topics = prependUnique(f.Projects[i].Topics, id)
				return true, nil
			}
		}
		f.Projects = append(f.Projects, Project{Root: root, Topics: []string{id}})
		return true, nil
	})
	if err != nil {
		return TopicMeta{}, err
	}
	return TopicMeta{ID: id, Title: title, CreatedAt: createdAt}, nil
}

// findTopicRoot locates which workspace (or global "") owns topicID.
func findTopicRoot(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return ""
	}
	f := LoadProjects()
	for _, id := range f.GlobalTopics {
		if id == topicID {
			return ""
		}
	}
	for _, p := range f.Projects {
		for _, id := range p.Topics {
			if id == topicID {
				return normalizeRoot(p.Root)
			}
		}
		// Title maps may still hold topics not listed (open-tab created).
		titles := loadTopicTitles(p.Root)
		if _, ok := titles[topicID]; ok {
			return normalizeRoot(p.Root)
		}
	}
	if _, ok := loadTopicTitles("")[topicID]; ok {
		return ""
	}
	return ""
}

// RenameTopic updates a topic display title. workspaceRoot may be empty; the
// store then resolves the owning project from desktop-projects.json.
func RenameTopic(workspaceRoot, topicID, title string) error {
	topicID = strings.TrimSpace(topicID)
	title = strings.TrimSpace(title)
	if topicID == "" || title == "" {
		return fmt.Errorf("topicId and title required")
	}
	root := normalizeRoot(workspaceRoot)
	if root == "" {
		root = findTopicRoot(topicID)
	}
	titles := loadTopicTitles(root)
	titles[topicID] = title
	return saveTopicTitles(root, titles)
}

// DeleteTopic marks a topic deleted and removes it from project topic lists.
func DeleteTopic(workspaceRoot, topicID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return fmt.Errorf("topicId required")
	}
	root := normalizeRoot(workspaceRoot)
	if root == "" && findTopicRoot(topicID) != "" {
		root = findTopicRoot(topicID)
	}
	// When findTopicRoot returns "" it may mean global OR unknown — still mark deleted.
	return updateProjects(func(f *File) (bool, error) {
		f.DeletedTopics = prependUnique(f.DeletedTopics, topicID)
		if root == "" {
			f.GlobalTopics = removeString(f.GlobalTopics, topicID)
			f.GlobalPinnedTopics = removeString(f.GlobalPinnedTopics, topicID)
			for i := range f.Projects {
				f.Projects[i].Topics = removeString(f.Projects[i].Topics, topicID)
				f.Projects[i].PinnedTopics = removeString(f.Projects[i].PinnedTopics, topicID)
			}
			return true, nil
		}
		for i := range f.Projects {
			if sameRoot(f.Projects[i].Root, root) {
				f.Projects[i].Topics = removeString(f.Projects[i].Topics, topicID)
				f.Projects[i].PinnedTopics = removeString(f.Projects[i].PinnedTopics, topicID)
				return true, nil
			}
		}
		return true, nil
	})
}

// TrashTopic is an alias of DeleteTopic for the desktop bridge name.
func TrashTopic(workspaceRoot, topicID string) error {
	return DeleteTopic(workspaceRoot, topicID)
}

// RemoveWorkspace drops a project from the sidebar registry (does not delete files).
func RemoveWorkspace(workspaceRoot string) error {
	root := normalizeRoot(workspaceRoot)
	if root == "" {
		return fmt.Errorf("workspaceRoot required")
	}
	return updateProjects(func(f *File) (bool, error) {
		next := make([]Project, 0, len(f.Projects))
		for _, p := range f.Projects {
			if !sameRoot(p.Root, root) {
				next = append(next, p)
			}
		}
		if len(next) == len(f.Projects) {
			return false, nil
		}
		f.Projects = next
		f.PinnedProjects = removeString(f.PinnedProjects, root)
		f.SidebarOrder = removeString(f.SidebarOrder, root)
		return true, nil
	})
}

// RenameProject sets a display title override for a project folder.
func RenameProject(workspaceRoot, title string) error {
	root := normalizeRoot(workspaceRoot)
	if root == "" {
		return fmt.Errorf("workspaceRoot required")
	}
	return updateProjects(func(f *File) (bool, error) {
		for i := range f.Projects {
			if sameRoot(f.Projects[i].Root, root) {
				f.Projects[i].Title = strings.TrimSpace(title)
				return true, nil
			}
		}
		f.Projects = append(f.Projects, Project{Root: root, Title: strings.TrimSpace(title), Topics: []string{"main"}})
		return true, nil
	})
}

// ReorderProjects persists a user-defined project order.
func ReorderProjects(workspaceRoots []string) error {
	return updateProjects(func(f *File) (bool, error) {
		order := make([]string, 0, len(workspaceRoots))
		for _, r := range workspaceRoots {
			r = normalizeRoot(r)
			if r != "" {
				order = append(order, r)
			}
		}
		f.SidebarOrder = order
		return true, nil
	})
}

// BuildTree merges desktop-projects.json, topic titles, open-tab hints, and
// session listings into a ProjectNode tree for the sidebar.
func BuildTree(openTabs []SessionHint, sessions []SessionHint) []Node {
	f := LoadProjects()
	// Index sessions by workspace + topic.
	type topicAgg struct {
		title      string
		session    string
		turns      int
		lastAct    int64
		running    bool
	}
	byRoot := map[string]map[string]*topicAgg{}
	ensure := func(root, topicID string) *topicAgg {
		root = normalizeRoot(root)
		if byRoot[root] == nil {
			byRoot[root] = map[string]*topicAgg{}
		}
		if byRoot[root][topicID] == nil {
			byRoot[root][topicID] = &topicAgg{title: "Session"}
		}
		return byRoot[root][topicID]
	}
	for _, s := range append(append([]SessionHint{}, sessions...), openTabs...) {
		root := normalizeRoot(s.WorkspaceRoot)
		if root == "" {
			continue
		}
		tid := strings.TrimSpace(s.TopicID)
		if tid == "" {
			tid = "main"
		}
		agg := ensure(root, tid)
		if s.TopicTitle != "" {
			agg.title = s.TopicTitle
		}
		if s.Path != "" {
			agg.session = s.Path
		}
		if s.Turns > agg.turns {
			agg.turns = s.Turns
		}
		if s.LastActivityAt > agg.lastAct {
			agg.lastAct = s.LastActivityAt
		}
		if s.Running {
			agg.running = true
		}
	}
	// Ensure registered projects appear even without sessions.
	for _, p := range f.Projects {
		root := normalizeRoot(p.Root)
		_ = ensure(root, "main")
		titles := loadTopicTitles(root)
		for _, tid := range p.Topics {
			agg := ensure(root, tid)
			if t := titles[tid]; t != "" {
				agg.title = t
			}
		}
	}
	// Register open-tab / session roots so they appear even if never pinned.
	for root := range byRoot {
		_ = EnsureProject(root)
	}
	f = LoadProjects()
	// Rebuild byRoot topic titles from refreshed project registry after EnsureProject.
	for _, p := range f.Projects {
		root := normalizeRoot(p.Root)
		titles := loadTopicTitles(root)
		for _, tid := range p.Topics {
			agg := ensure(root, tid)
			if t := titles[tid]; t != "" {
				agg.title = t
			}
		}
	}

	deleted := map[string]bool{}
	for _, d := range f.DeletedTopics {
		deleted[d] = true
	}
	pinnedSet := map[string]bool{}
	for _, r := range f.PinnedProjects {
		pinnedSet[normalizeRoot(r)] = true
	}

	// Order projects: sidebar order, then pinned, then remaining.
	order := make([]string, 0, len(f.Projects)+len(byRoot))
	seen := map[string]bool{}
	addRoot := func(r string) {
		r = normalizeRoot(r)
		if r == "" || seen[r] {
			return
		}
		seen[r] = true
		order = append(order, r)
	}
	for _, r := range f.SidebarOrder {
		addRoot(r)
	}
	for _, r := range f.PinnedProjects {
		addRoot(r)
	}
	for _, p := range f.Projects {
		addRoot(p.Root)
	}
	for r := range byRoot {
		addRoot(r)
	}

	out := make([]Node, 0, len(order))
	for _, root := range order {
		pTitle := filepath.Base(root)
		var color string
		var topicIDs []string
		pinnedTopics := map[string]bool{}
		for _, p := range f.Projects {
			if sameRoot(p.Root, root) {
				if p.Title != "" {
					pTitle = p.Title
				}
				color = p.Color
				topicIDs = append(topicIDs, p.Topics...)
				for _, t := range p.PinnedTopics {
					pinnedTopics[t] = true
				}
				break
			}
		}
		topics := byRoot[root]
		if topics == nil {
			topics = map[string]*topicAgg{}
		}
		// Union topic IDs from registry + sessions.
		for tid := range topics {
			topicIDs = prependUnique(topicIDs, tid)
		}
		if len(topicIDs) == 0 {
			topicIDs = []string{"main"}
		}
		titles := loadTopicTitles(root)
		children := make([]Node, 0, len(topicIDs))
		for _, tid := range topicIDs {
			if deleted[tid] {
				continue
			}
			agg := topics[tid]
			if agg == nil {
				agg = &topicAgg{title: "Session"}
			}
			label := agg.title
			if t := titles[tid]; t != "" {
				label = t
			}
			children = append(children, Node{
				Key:            "topic_" + tid,
				Kind:           "topic",
				Label:          label,
				Root:           root,
				TopicID:        tid,
				SessionPath:    agg.session,
				ProjectColor:   color,
				Turns:          agg.turns,
				LastActivityAt: agg.lastAct,
				Open:           true,
				Running:        agg.running,
				Pinned:         pinnedTopics[tid],
			})
		}
		out = append(out, Node{
			Key:          "project_" + root,
			Kind:         "project",
			Label:        pTitle,
			Root:         root,
			ProjectColor: color,
			Open:         true,
			Pinned:       pinnedSet[root],
			Children:     children,
		})
	}
	return out
}

func prependUnique(list []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return list
	}
	out := make([]string, 0, len(list)+1)
	out = append(out, v)
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func removeString(list []string, v string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
