package catalog

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/remote/protocol"
)

// WorkspaceCloseCapability is an instance-bound, single-use proof that the
// daemon has already acquired its RuntimeManager reservation. Its fields are
// deliberately unexported so callers cannot forge a bypass token.
type WorkspaceCloseCapability struct {
	catalog   *Catalog
	workspace protocol.WorkspaceID
	used      bool
}

// IssueWorkspaceCloseCapability must be called only after RuntimeManager has
// frozen the matching workspace. CloseWorkspaceReserved validates and consumes
// the capability under the catalog lock.
func (c *Catalog) IssueWorkspaceCloseCapability(workspaceID protocol.WorkspaceID) *WorkspaceCloseCapability {
	return &WorkspaceCloseCapability{catalog: c, workspace: workspaceID}
}

func (c *Catalog) Browse(params protocol.WorkspaceBrowseParams) (protocol.WorkspaceBrowseResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.WorkspaceBrowseResult{}, err
	}
	limit, err := normalizedLimit(params.Limit)
	if err != nil {
		return protocol.WorkspaceBrowseResult{}, err
	}
	if params.DirectoryRef != "" && strings.TrimSpace(params.TypedPath) != "" {
		return protocol.WorkspaceBrowseResult{}, fmt.Errorf("catalog: directoryRef and typedPath are mutually exclusive")
	}

	directory, cursorState, err := c.resolveBrowsePageLocked(params)
	if err != nil {
		return protocol.WorkspaceBrowseResult{}, err
	}
	start := cursorState.Offset
	if start > len(cursorState.Names) {
		return protocol.WorkspaceBrowseResult{}, catalogError(protocol.ErrStaleCursor, errors.New("browse offset exceeds snapshot"))
	}
	end := start + limit
	if end > len(cursorState.Names) {
		end = len(cursorState.Names)
	}
	entries := make([]protocol.DirectoryItem, 0, end-start)
	for _, name := range cursorState.Names[start:end] {
		candidate := filepath.Join(directory, name)
		canonical, pathErr := canonicalExistingDirectory(candidate)
		if pathErr != nil {
			// The page snapshot is allowed to outlive a concurrent filesystem
			// deletion. Missing/inaccessible children disappear from this page;
			// the selected directory itself still receives typed errors above.
			continue
		}
		ref, refErr := c.directoryRefLocked(canonical)
		if refErr != nil {
			return protocol.WorkspaceBrowseResult{}, refErr
		}
		entries = append(entries, protocol.DirectoryItem{
			DirectoryRef: ref,
			Name:         name,
			DisplayPath:  canonical,
		})
	}
	hasMore := end < len(cursorState.Names)
	var next protocol.Cursor
	if hasMore {
		cursorState.Offset = end
		next, err = c.storeCursorLocked(cursorState)
		if err != nil {
			return protocol.WorkspaceBrowseResult{}, err
		}
	}
	item, err := c.directoryItemLocked(directory, true)
	if err != nil {
		return protocol.WorkspaceBrowseResult{}, err
	}
	return protocol.WorkspaceBrowseResult{Directory: item, Entries: entries, HasMore: hasMore, NextCursor: next}, nil
}

func (c *Catalog) resolveBrowsePageLocked(params protocol.WorkspaceBrowseParams) (string, cursorRecord, error) {
	if params.Cursor != "" {
		state, ok := c.cursors[params.Cursor]
		if !ok || state.Method != "workspace/browse" {
			return "", cursorRecord{}, catalogError(protocol.ErrStaleCursor, errors.New("unknown browse cursor"))
		}
		directory := state.Directory
		if params.DirectoryRef != "" {
			selected, ok := c.directoryRefs[params.DirectoryRef]
			if !ok {
				return "", cursorRecord{}, catalogError(protocol.ErrStaleDirectoryRef, errors.New("unknown directory reference"))
			}
			if pathKey(selected) != pathKey(directory) {
				return "", cursorRecord{}, catalogError(protocol.ErrStaleCursor, errors.New("cursor is bound to a different directory"))
			}
		}
		if strings.TrimSpace(params.TypedPath) != "" {
			selected, err := c.resolveTypedDirectoryLocked(params.TypedPath)
			if err != nil {
				return "", cursorRecord{}, err
			}
			if pathKey(selected) != pathKey(directory) {
				return "", cursorRecord{}, catalogError(protocol.ErrStaleCursor, errors.New("cursor is bound to a different directory"))
			}
		}
		if _, err := canonicalExistingDirectory(directory); err != nil {
			return "", cursorRecord{}, err
		}
		return directory, state, nil
	}

	directory := c.userHome
	var err error
	if params.DirectoryRef != "" {
		var ok bool
		directory, ok = c.directoryRefs[params.DirectoryRef]
		if !ok {
			return "", cursorRecord{}, catalogError(protocol.ErrStaleDirectoryRef, errors.New("unknown directory reference"))
		}
		directory, err = canonicalExistingDirectory(directory)
	} else if strings.TrimSpace(params.TypedPath) != "" {
		directory, err = c.resolveTypedDirectoryLocked(params.TypedPath)
	} else {
		directory, err = canonicalExistingDirectory(directory)
	}
	if err != nil {
		return "", cursorRecord{}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", cursorRecord{}, classifyDirectoryError(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		candidate := filepath.Join(directory, entry.Name())
		if _, err := canonicalExistingDirectory(candidate); err == nil {
			names = append(names, entry.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		lowerI, lowerJ := strings.ToLower(names[i]), strings.ToLower(names[j])
		if lowerI == lowerJ {
			return names[i] < names[j]
		}
		return lowerI < lowerJ
	})
	return directory, cursorRecord{
		Method: "workspace/browse", Binding: pathKey(directory), Directory: directory, Names: names,
	}, nil
}

func (c *Catalog) resolveTypedDirectoryLocked(typed string) (string, error) {
	path := strings.TrimSpace(typed)
	if path == "~" {
		path = c.userHome
	} else if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		path = filepath.Join(c.userHome, path[2:])
	} else if !filepath.IsAbs(path) {
		// Browse is Host-level and has no active workspace. Resolve relative user
		// input against the SSH user's home rather than process cwd.
		path = filepath.Join(c.userHome, path)
	}
	return canonicalExistingDirectory(path)
}

func canonicalExistingDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", catalogError(protocol.ErrDirectoryNotFound, errors.New("empty directory path"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", classifyDirectoryError(err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", classifyDirectoryError(err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", classifyDirectoryError(err)
	}
	if !info.IsDir() {
		return "", catalogError(protocol.ErrNotDirectory, errors.New("selected path is not a directory"))
	}
	// Opening the directory distinguishes execute/read denial that Stat alone
	// may not catch and guarantees browse/open use the same permission preflight.
	handle, err := os.Open(canonical)
	if err != nil {
		return "", classifyDirectoryError(err)
	}
	_, readErr := handle.Readdirnames(1)
	closeErr := handle.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", classifyDirectoryError(readErr)
	}
	if closeErr != nil {
		return "", classifyDirectoryError(closeErr)
	}
	return filepath.Clean(canonical), nil
}

func classifyDirectoryError(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return catalogError(protocol.ErrDirectoryNotFound, err)
	case errors.Is(err, fs.ErrPermission), os.IsPermission(err):
		return catalogError(protocol.ErrPermissionDenied, err)
	default:
		return catalogError(protocol.ErrQueryFailed, err)
	}
}

func (c *Catalog) directoryRefLocked(canonical string) (protocol.DirectoryRef, error) {
	key := pathKey(canonical)
	if ref := c.pathToDirectoryRef[key]; ref != "" {
		return ref, nil
	}
	id, err := c.nextIDLocked("directory")
	if err != nil {
		return "", err
	}
	ref := protocol.DirectoryRef(id)
	c.pathToDirectoryRef[key] = ref
	c.directoryRefs[ref] = canonical
	return ref, nil
}

func (c *Catalog) directoryItemLocked(canonical string, includeParent bool) (protocol.DirectoryItem, error) {
	ref, err := c.directoryRefLocked(canonical)
	if err != nil {
		return protocol.DirectoryItem{}, err
	}
	item := protocol.DirectoryItem{DirectoryRef: ref, Name: workspaceName(canonical), DisplayPath: canonical}
	if includeParent {
		parent := filepath.Dir(canonical)
		if pathKey(parent) != pathKey(canonical) {
			if parent, err = canonicalExistingDirectory(parent); err == nil {
				item.ParentRef, err = c.directoryRefLocked(parent)
				if err != nil {
					return protocol.DirectoryItem{}, err
				}
			}
		}
	}
	return item, nil
}

func (c *Catalog) storeCursorLocked(state cursorRecord) (protocol.Cursor, error) {
	id, err := c.nextIDLocked("cursor")
	if err != nil {
		return "", err
	}
	cursor := protocol.Cursor(id)
	c.cursors[cursor] = state
	return cursor, nil
}

func (c *Catalog) OpenWorkspace(params protocol.WorkspaceOpenParams) (protocol.WorkspaceOpenResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.WorkspaceOpenResult{}, err
	}
	path, ok := c.directoryRefs[params.PrimaryDirectoryRef]
	if !ok {
		return protocol.WorkspaceOpenResult{}, catalogError(protocol.ErrStaleDirectoryRef, errors.New("unknown primary directory reference"))
	}
	canonical, err := canonicalExistingDirectory(path)
	if err != nil {
		return protocol.WorkspaceOpenResult{}, err
	}
	if existingID := c.pathToWorkspace[pathKey(canonical)]; existingID != "" {
		record := c.state.Workspaces[existingID]
		if record.Open {
			return protocol.WorkspaceOpenResult{Workspace: workspaceSummary(record), Disposition: protocol.WorkspaceAlreadyOpen}, nil
		}
		err := c.mutateLocked(func() error {
			record.Open = true
			record.UpdatedAtMs = nonnegativeUnixMilli(c.now())
			c.state.Workspaces[existingID] = record
			return nil
		})
		if err != nil {
			return protocol.WorkspaceOpenResult{}, err
		}
		return protocol.WorkspaceOpenResult{Workspace: workspaceSummary(record), Disposition: protocol.WorkspaceOpened}, nil
	}
	id, err := c.nextIDLocked("workspace")
	if err != nil {
		return protocol.WorkspaceOpenResult{}, err
	}
	now := nonnegativeUnixMilli(c.now())
	record := workspaceRecord{ID: protocol.WorkspaceID(id), CanonicalPath: canonical, Open: true, CreatedAtMs: now, UpdatedAtMs: now}
	if err := c.mutateLocked(func() error {
		c.state.Workspaces[record.ID] = record
		c.pathToWorkspace[pathKey(canonical)] = record.ID
		if c.state.Topics[record.ID] == nil {
			c.state.Topics[record.ID] = make(map[protocol.TopicID]topicRecord)
		}
		return nil
	}); err != nil {
		return protocol.WorkspaceOpenResult{}, err
	}
	return protocol.WorkspaceOpenResult{Workspace: workspaceSummary(record), Disposition: protocol.WorkspaceOpened}, nil
}

func (c *Catalog) ListWorkspaces(params protocol.WorkspaceListParams) (protocol.WorkspaceListResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.WorkspaceListResult{}, err
	}
	limit, err := normalizedLimit(params.Limit)
	if err != nil {
		return protocol.WorkspaceListResult{}, err
	}
	ids := sortedWorkspaceIDs(c.state.Workspaces, true)
	start, err := c.pageStartLocked("workspace/list", "open", params.Cursor, c.state.Revision)
	if err != nil {
		return protocol.WorkspaceListResult{}, err
	}
	end := start + limit
	if end > len(ids) {
		end = len(ids)
	}
	items := make([]protocol.WorkspaceSummary, 0, end-start)
	for _, id := range ids[start:end] {
		items = append(items, workspaceSummary(c.state.Workspaces[id]))
	}
	hasMore := end < len(ids)
	var next protocol.Cursor
	if hasMore {
		next, err = c.storeCursorLocked(cursorRecord{Method: "workspace/list", Binding: "open", Revision: c.state.Revision, Offset: end})
		if err != nil {
			return protocol.WorkspaceListResult{}, err
		}
	}
	return protocol.WorkspaceListResult{Items: items, HasMore: hasMore, NextCursor: next}, nil
}

func (c *Catalog) pageStartLocked(method, binding string, cursor protocol.Cursor, revision uint64) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	state, ok := c.cursors[cursor]
	if !ok || state.Method != method || state.Binding != binding || state.Revision != revision || state.Offset < 0 {
		return 0, catalogError(protocol.ErrStaleCursor, errors.New("cursor binding or catalog revision changed"))
	}
	return state.Offset, nil
}

func (c *Catalog) CloseWorkspace(params protocol.WorkspaceCloseParams) (protocol.WorkspaceCloseResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A non-nil inspector means runtimes may exist. A probe followed by a
	// durable close is inherently racy, so only the daemon's explicit
	// RuntimeManager reservation may use the reserved entry point below.
	if c.runtimeInspector != nil {
		return protocol.WorkspaceCloseResult{}, catalogError(protocol.ErrWorkspaceInUse, errors.New("workspace close requires an atomic RuntimeManager reservation"))
	}
	return c.closeWorkspaceReservedLocked(params)
}

// CloseWorkspaceReserved durably closes a workspace after the caller has
// frozen RuntimeManager admission and every matching Session actor.
func (c *Catalog) CloseWorkspaceReserved(params protocol.WorkspaceCloseParams, capability *WorkspaceCloseCapability) (protocol.WorkspaceCloseResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if capability == nil || capability.catalog != c || capability.workspace != params.WorkspaceID || capability.used {
		return protocol.WorkspaceCloseResult{}, catalogError(protocol.ErrWorkspaceInUse, errors.New("workspace close reservation capability is missing or invalid"))
	}
	capability.used = true
	return c.closeWorkspaceReservedLocked(params)
}

func (c *Catalog) closeWorkspaceReservedLocked(params protocol.WorkspaceCloseParams) (protocol.WorkspaceCloseResult, error) {
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.WorkspaceCloseResult{}, err
	}
	record, ok := c.state.Workspaces[params.WorkspaceID]
	if !ok {
		return protocol.WorkspaceCloseResult{}, catalogError(protocol.ErrWorkspaceNotFound, errors.New("unknown workspace identity"))
	}
	if !record.Open {
		return protocol.WorkspaceCloseResult{Disposition: protocol.WorkspaceAlreadyClosed}, nil
	}
	if err := c.mutateLocked(func() error {
		record.Open = false
		record.UpdatedAtMs = nonnegativeUnixMilli(c.now())
		c.state.Workspaces[params.WorkspaceID] = record
		return nil
	}); err != nil {
		return protocol.WorkspaceCloseResult{}, err
	}
	return protocol.WorkspaceCloseResult{Disposition: protocol.WorkspaceClosed}, nil
}
