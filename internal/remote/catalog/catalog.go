// Package catalog owns persistent Remote workspace, Topic, and Session
// identity. It never changes process cwd and never exposes Host artifact paths
// through protocol list DTOs.
package catalog

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
	"reasonix/internal/remote/protocol"
)

const (
	diskVersion       = 1
	defaultPageLimit  = 200
	maximumPageLimit  = 1000
	defaultTopicTitle = "新的会话"
)

// ProfileResolver resolves a partial client selection against authoritative
// Host configuration. Catalog requires the returned value to be complete
// before a Session artifact is created or migrated.
type ProfileResolver interface {
	ResolveProfile(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error)
}

// WorkspaceCatalogProvider exposes the credential-free model/profile catalog
// computed from authoritative Host configuration for one canonical workspace.
// It is optional only for narrow catalog tests; production Host composition
// requires it for the catalog/workspace wire method.
type WorkspaceCatalogProvider interface {
	WorkspaceCatalog(context.Context, string) (protocol.WorkspaceCatalogResult, error)
}

type ProfileResolverFunc func(context.Context, string, protocol.ProfileSelection) (protocol.ResolvedProfile, error)

func (f ProfileResolverFunc) ResolveProfile(ctx context.Context, workspaceRoot string, selection protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
	return f(ctx, workspaceRoot, selection)
}

// RuntimeInspector supplies live-only status without making the durable
// catalog own runtime lifecycle or subscriptions.
type RuntimeInspector interface {
	WorkspaceInUse(protocol.WorkspaceID) bool
	SessionSummary(protocol.RuntimeTarget) (*protocol.SessionRuntimeSummary, bool)
}

// RuntimeTargetResolver is the deliberately narrow boundary consumed by Host
// runtime construction.  Protocol adapters hand it only opaque identities;
// filesystem paths and the resolved Host profile are returned exclusively to
// Host-side code through ResolvedSession.
type RuntimeTargetResolver interface {
	ResolveRuntimeTarget(context.Context, protocol.RuntimeTarget) (ResolvedSession, error)
}

// Options keeps path roots and nondeterminism injectable for persistence,
// collision, and permission tests.
type Options struct {
	StateDir          string
	UserHome          string
	SessionDir        func(canonicalWorkspace string) string
	NewOpaqueID       func(kind string) (string, error)
	Now               func() time.Time
	ProfileResolver   ProfileResolver
	RuntimeInspector  RuntimeInspector
	DefaultTopicTitle string
	// RemoveAll is injectable for deterministic staged-cleanup failure tests.
	// Production defaults to os.RemoveAll.
	RemoveAll func(string) error
}

type workspaceRecord struct {
	ID            protocol.WorkspaceID `json:"workspaceId"`
	CanonicalPath string               `json:"canonicalPath"`
	Open          bool                 `json:"open"`
	CreatedAtMs   int64                `json:"createdAtMs"`
	UpdatedAtMs   int64                `json:"updatedAtMs"`
}

type topicRecord struct {
	ID          protocol.TopicID `json:"topicId"`
	Title       string           `json:"title"`
	CreatedAtMs int64            `json:"createdAtMs"`
	// Trashed is a durable tombstone for topic/trash.  There is no separate
	// topic/restore method in V1; restoring any member Session makes its
	// original Topic visible again.
	Trashed     bool  `json:"trashed,omitempty"`
	TrashedAtMs int64 `json:"trashedAtMs,omitempty"`
}

type sessionRecord struct {
	ID          protocol.SessionID   `json:"sessionId"`
	WorkspaceID protocol.WorkspaceID `json:"workspaceId"`
	Path        string               `json:"path,omitempty"`
	TrashPath   string               `json:"trashPath,omitempty"`
	TopicID     protocol.TopicID     `json:"topicId"`
	TrashedAtMs int64                `json:"trashedAtMs,omitempty"`
}

type diskState struct {
	Version           int                                                       `json:"version"`
	Revision          uint64                                                    `json:"revision"`
	Workspaces        map[protocol.WorkspaceID]workspaceRecord                  `json:"workspaces"`
	Topics            map[protocol.WorkspaceID]map[protocol.TopicID]topicRecord `json:"topics"`
	Sessions          map[protocol.SessionID]sessionRecord                      `json:"sessions"`
	RetiredSessionIDs map[protocol.SessionID]bool                               `json:"retiredSessionIds"`
}

type cursorRecord struct {
	Method    string
	Binding   string
	Revision  uint64
	Offset    int
	Directory string
	Names     []string
}

// Catalog is safe for concurrent use. All mutations and sidecar migrations are
// serialized so two Remote requests cannot allocate duplicate identities or
// overwrite each other's durable registry update.
type Catalog struct {
	mu sync.Mutex

	hostEpoch         protocol.HostEpoch
	statePath         string
	userHome          string
	sessionDir        func(string) string
	newOpaqueID       func(string) (string, error)
	now               func() time.Time
	profileResolver   ProfileResolver
	runtimeInspector  RuntimeInspector
	defaultTopicTitle string
	removeAll         func(string) error

	state              diskState
	pathToWorkspace    map[string]protocol.WorkspaceID
	directoryRefs      map[protocol.DirectoryRef]string
	pathToDirectoryRef map[string]protocol.DirectoryRef
	cursors            map[protocol.Cursor]cursorRecord
	issued             map[string]struct{}
}

func New(hostEpoch protocol.HostEpoch, options Options) (*Catalog, error) {
	if strings.TrimSpace(string(hostEpoch)) == "" {
		return nil, errors.New("catalog: host epoch is empty")
	}
	stateDir := strings.TrimSpace(options.StateDir)
	if stateDir == "" {
		home := config.ReasonixHomeDir()
		if home == "" {
			return nil, errors.New("catalog: Reasonix Home is unavailable")
		}
		stateDir = filepath.Join(home, "remote", "catalog")
	}
	stateDir, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, fmt.Errorf("catalog: resolve state dir: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("catalog: create state dir: %w", err)
	}
	if err := ensurePrivateDirectory(stateDir); err != nil {
		return nil, fmt.Errorf("catalog: protect state dir: %w", err)
	}
	userHome := strings.TrimSpace(options.UserHome)
	if userHome == "" {
		userHome, err = os.UserHomeDir()
		if err != nil || strings.TrimSpace(userHome) == "" {
			return nil, fmt.Errorf("catalog: resolve user home: %w", err)
		}
	}
	userHome, err = filepath.Abs(userHome)
	if err != nil {
		return nil, fmt.Errorf("catalog: resolve user home: %w", err)
	}
	if options.SessionDir == nil {
		options.SessionDir = config.ProjectSessionDir
	}
	if options.NewOpaqueID == nil {
		options.NewOpaqueID = randomOpaqueID
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.RemoveAll == nil {
		options.RemoveAll = os.RemoveAll
	}
	topicTitle := strings.TrimSpace(options.DefaultTopicTitle)
	if topicTitle == "" {
		topicTitle = defaultTopicTitle
	}
	c := &Catalog{
		hostEpoch:          hostEpoch,
		statePath:          filepath.Join(stateDir, "catalog-v1.json"),
		userHome:           userHome,
		sessionDir:         options.SessionDir,
		newOpaqueID:        options.NewOpaqueID,
		now:                options.Now,
		profileResolver:    options.ProfileResolver,
		runtimeInspector:   options.RuntimeInspector,
		defaultTopicTitle:  topicTitle,
		removeAll:          options.RemoveAll,
		pathToWorkspace:    make(map[string]protocol.WorkspaceID),
		directoryRefs:      make(map[protocol.DirectoryRef]string),
		pathToDirectoryRef: make(map[string]protocol.DirectoryRef),
		cursors:            make(map[protocol.Cursor]cursorRecord),
		issued:             make(map[string]struct{}),
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

func randomOpaqueID(kind string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	kind = strings.Trim(strings.ToLower(kind), "_-")
	if kind == "" {
		kind = "id"
	}
	return kind + "_" + hex.EncodeToString(raw[:]), nil
}

// SetRuntimeInspector connects the already-created daemon RuntimeManager after
// catalog construction. Host composition necessarily creates the catalog first
// because the runtime factory resolves targets through it.
func (c *Catalog) SetRuntimeInspector(inspector RuntimeInspector) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.runtimeInspector = inspector
	c.mu.Unlock()
}

// Revision returns the current opaque durable catalog revision for
// catalog/changed notifications.
func (c *Catalog) Revision() protocol.CatalogRevision {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return protocol.CatalogRevision(fmt.Sprintf("catalog_%d", c.state.Revision))
}

// AdvanceRevision invalidates daemon-wide catalog projections that are
// persisted outside the Remote catalog registry (for example Memory, Skills,
// and AutoResearch). The Host epoch scopes this monotonic value, so a catalog
// rewrite is unnecessary solely for an invalidation; the next durable catalog
// mutation persists the advanced revision with its own state change.
func (c *Catalog) AdvanceRevision() protocol.CatalogRevision {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Revision++
	return protocol.CatalogRevision(fmt.Sprintf("catalog_%d", c.state.Revision))
}

func (c *Catalog) load() error {
	if info, err := os.Lstat(c.statePath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("catalog: registry is not a regular file")
		}
		if err := os.Chmod(c.statePath, 0o600); err != nil {
			return fmt.Errorf("catalog: protect registry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("catalog: inspect registry: %w", err)
	}
	b, err := os.ReadFile(c.statePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("catalog: read registry: %w", err)
		}
		c.state = newDiskState()
		return nil
	}
	if err := json.Unmarshal(b, &c.state); err != nil {
		return fmt.Errorf("catalog: decode registry: %w", err)
	}
	if c.state.Version != diskVersion {
		return fmt.Errorf("catalog: unsupported registry version %d", c.state.Version)
	}
	c.normalizeState()
	for id, record := range c.state.Workspaces {
		path := filepath.Clean(record.CanonicalPath)
		if id == "" || record.ID != id || strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || record.CreatedAtMs < 0 || record.UpdatedAtMs < 0 {
			return fmt.Errorf("catalog: invalid persisted workspace record")
		}
		if previous, exists := c.pathToWorkspace[pathKey(path)]; exists && previous != id {
			return fmt.Errorf("catalog: duplicate canonical workspace path")
		}
		record.CanonicalPath = path
		c.state.Workspaces[id] = record
		c.pathToWorkspace[pathKey(path)] = id
		c.issued[string(id)] = struct{}{}
	}
	for workspaceID, topics := range c.state.Topics {
		if _, ok := c.state.Workspaces[workspaceID]; !ok {
			return fmt.Errorf("catalog: topics reference missing workspace")
		}
		for id, record := range topics {
			if id == "" || record.ID != id || strings.TrimSpace(record.Title) == "" || record.CreatedAtMs < 0 || record.TrashedAtMs < 0 {
				return fmt.Errorf("catalog: invalid persisted topic record")
			}
			c.issued[string(id)] = struct{}{}
		}
	}
	for id, record := range c.state.Sessions {
		if id == "" || record.ID != id {
			return fmt.Errorf("catalog: invalid persisted session record")
		}
		if _, ok := c.state.Workspaces[record.WorkspaceID]; !ok {
			return fmt.Errorf("catalog: session references missing workspace")
		}
		if record.TopicID == "" {
			return fmt.Errorf("catalog: session has no Topic identity")
		}
		if _, ok := c.state.Topics[record.WorkspaceID][record.TopicID]; !ok {
			return fmt.Errorf("catalog: session references missing Topic")
		}
		if strings.TrimSpace(record.Path) == "" || !filepath.IsAbs(record.Path) || record.TrashedAtMs < 0 {
			return fmt.Errorf("catalog: invalid persisted Session path")
		}
		if record.TrashPath != "" && !filepath.IsAbs(record.TrashPath) {
			return fmt.Errorf("catalog: invalid persisted trash path")
		}
		c.issued[string(id)] = struct{}{}
	}
	for id := range c.state.RetiredSessionIDs {
		if id == "" {
			return fmt.Errorf("catalog: invalid retired Session identity")
		}
		if _, live := c.state.Sessions[id]; live {
			return fmt.Errorf("catalog: Session identity is both live and retired")
		}
		c.issued[string(id)] = struct{}{}
	}
	return nil
}

func newDiskState() diskState {
	return diskState{
		Version:           diskVersion,
		Workspaces:        make(map[protocol.WorkspaceID]workspaceRecord),
		Topics:            make(map[protocol.WorkspaceID]map[protocol.TopicID]topicRecord),
		Sessions:          make(map[protocol.SessionID]sessionRecord),
		RetiredSessionIDs: make(map[protocol.SessionID]bool),
	}
}

func (c *Catalog) normalizeState() {
	if c.state.Workspaces == nil {
		c.state.Workspaces = make(map[protocol.WorkspaceID]workspaceRecord)
	}
	if c.state.Topics == nil {
		c.state.Topics = make(map[protocol.WorkspaceID]map[protocol.TopicID]topicRecord)
	}
	if c.state.Sessions == nil {
		c.state.Sessions = make(map[protocol.SessionID]sessionRecord)
	}
	if c.state.RetiredSessionIDs == nil {
		c.state.RetiredSessionIDs = make(map[protocol.SessionID]bool)
	}
}

func (c *Catalog) saveLocked() error {
	dir := filepath.Dir(c.statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("catalog: create registry directory: %w", err)
	}
	// This directory contains canonical Host paths and durable opaque
	// identities. Keep an existing overly-permissive directory from silently
	// weakening the 0700 contract.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("catalog: protect registry directory: %w", err)
	}
	b, err := json.MarshalIndent(c.state, "", "  ")
	if err != nil {
		return fmt.Errorf("catalog: encode registry: %w", err)
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, ".catalog-*.tmp")
	if err != nil {
		return fmt.Errorf("catalog: create registry temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("catalog: chmod registry temp: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		cleanup()
		return fmt.Errorf("catalog: write registry temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("catalog: sync registry temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("catalog: close registry temp: %w", err)
	}
	if err := fileutil.ReplaceFile(tmpPath, c.statePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("catalog: replace registry: %w", err)
	}
	return nil
}

func (c *Catalog) mutateLocked(fn func() error) error {
	before, err := cloneState(c.state)
	if err != nil {
		return err
	}
	if err := fn(); err != nil {
		c.state = before
		c.rebuildPathIndexLocked()
		return err
	}
	c.state.Revision++
	if err := c.saveLocked(); err != nil {
		c.state = before
		c.rebuildPathIndexLocked()
		return catalogError(protocol.ErrSessionPersistFailed, err)
	}
	return nil
}

func cloneState(in diskState) (diskState, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return diskState{}, err
	}
	var out diskState
	if err := json.Unmarshal(b, &out); err != nil {
		return diskState{}, err
	}
	return out, nil
}

func (c *Catalog) rebuildPathIndexLocked() {
	c.pathToWorkspace = make(map[string]protocol.WorkspaceID, len(c.state.Workspaces))
	for id, record := range c.state.Workspaces {
		c.pathToWorkspace[pathKey(record.CanonicalPath)] = id
	}
}

func (c *Catalog) nextIDLocked(kind string) (string, error) {
	for attempt := 0; attempt < 32; attempt++ {
		id, err := c.newOpaqueID(kind)
		if err != nil {
			return "", fmt.Errorf("catalog: generate %s identity: %w", kind, err)
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := c.issued[id]; exists {
			continue
		}
		c.issued[id] = struct{}{}
		return id, nil
	}
	return "", fmt.Errorf("catalog: %s identity generator repeatedly collided", kind)
}

func (c *Catalog) validateHostEpochLocked(epoch protocol.HostEpoch) error {
	if epoch != c.hostEpoch {
		return catalogError(protocol.ErrStaleHostEpoch, fmt.Errorf("expected current Host epoch"))
	}
	return nil
}

func normalizedLimit(limit *int) (int, error) {
	if limit == nil {
		return defaultPageLimit, nil
	}
	if *limit < 1 || *limit > maximumPageLimit {
		return 0, fmt.Errorf("page limit must be between 1 and %d", maximumPageLimit)
	}
	return *limit, nil
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if isCaseInsensitivePlatform() {
		path = strings.ToLower(path)
	}
	return path
}

func isCaseInsensitivePlatform() bool {
	return os.PathSeparator == '\\'
}

func workspaceSummary(record workspaceRecord) protocol.WorkspaceSummary {
	return protocol.WorkspaceSummary{
		WorkspaceID: record.ID,
		Name:        workspaceName(record.CanonicalPath),
		DisplayPath: record.CanonicalPath,
	}
}

func workspaceName(path string) string {
	clean := filepath.Clean(path)
	name := filepath.Base(clean)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return clean
	}
	return name
}

func nonnegativeUnixMilli(value time.Time) int64 {
	millis := value.UTC().UnixMilli()
	if millis < 0 {
		return 0
	}
	return millis
}

func sortedWorkspaceIDs(records map[protocol.WorkspaceID]workspaceRecord, openOnly bool) []protocol.WorkspaceID {
	ids := make([]protocol.WorkspaceID, 0, len(records))
	for id, record := range records {
		if !openOnly || record.Open {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := records[ids[i]], records[ids[j]]
		if a.UpdatedAtMs != b.UpdatedAtMs {
			return a.UpdatedAtMs > b.UpdatedAtMs
		}
		return ids[i] < ids[j]
	})
	return ids
}
