package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"reasonix/internal/fileutil"
	"reasonix/internal/runtimeapi"
)

const (
	remoteLayoutVersion          = 1
	remoteLayoutMaxBytes         = 1 << 20
	remoteLayoutMaxWorkspaces    = 4096
	remoteLayoutMaxPinnedTopics  = 16384
	remoteLayoutMaxOpaqueIDBytes = 1024
	remoteLayoutMaxTitleBytes    = 512
	remoteLayoutReferencePrefix  = "remote-layout_"
	remoteLayoutReferenceSuffix  = ".json"
)

var (
	// ErrRemoteLayoutCorrupt means an existing target layout cannot be trusted.
	// Mutations fail closed and never overwrite the only persisted layout.
	ErrRemoteLayoutCorrupt = errors.New("Remote Desktop layout is corrupt")
	// ErrRemoteLayoutUnsafe means a layout reference or file could escape the
	// private per-user store, is a link/non-regular file, or is not mode 0600.
	ErrRemoteLayoutUnsafe = errors.New("Remote Desktop layout is unsafe")

	remoteLayoutTargetLocks sync.Map // host-store path + Host ID -> *sync.Mutex
)

// remoteLayoutDocument contains Desktop-only presentation preferences. All
// keys are opaque Host identities: they are compared exactly and are never
// cleaned, normalized, joined, or passed to a Desktop filesystem API.
type remoteLayoutDocument struct {
	Version          int                 `json:"version"`
	HostID           string              `json:"hostId"`
	WorkspaceTitles  map[string]string   `json:"workspaceTitles,omitempty"`
	WorkspaceColors  map[string]string   `json:"workspaceColors,omitempty"`
	WorkspaceOrder   []string            `json:"workspaceOrder,omitempty"`
	PinnedWorkspaces []string            `json:"pinnedWorkspaces,omitempty"`
	PinnedTopics     map[string][]string `json:"pinnedTopics,omitempty"`
}

func newRemoteLayoutDocument(hostID string) remoteLayoutDocument {
	return remoteLayoutDocument{
		Version: remoteLayoutVersion, HostID: hostID,
		WorkspaceTitles: make(map[string]string), WorkspaceColors: make(map[string]string),
		PinnedTopics: make(map[string][]string),
	}
}

func remoteLayoutRefForHost(hostID string) (string, error) {
	if err := ValidateRemoteHostEntryID(hostID); err != nil {
		return "", err
	}
	entropy := strings.TrimPrefix(hostID, "host_")
	return remoteLayoutReferencePrefix + entropy + remoteLayoutReferenceSuffix, nil
}

func remoteLayoutPath(store *RemoteHostStore, host RemoteHostEntry) (string, error) {
	if store == nil {
		return "", errors.New("Remote Host store is unavailable")
	}
	expected, err := remoteLayoutRefForHost(host.ID)
	if err != nil {
		return "", err
	}
	if host.LayoutRef != expected || filepath.Base(host.LayoutRef) != host.LayoutRef {
		return "", fmt.Errorf("%w: invalid layoutRef for Host %q", ErrRemoteLayoutUnsafe, host.ID)
	}
	path := filepath.Join(filepath.Dir(store.Path()), host.LayoutRef)
	if filepath.Dir(path) != filepath.Dir(store.Path()) {
		return "", fmt.Errorf("%w: layoutRef escapes the Host store directory", ErrRemoteLayoutUnsafe)
	}
	return path, nil
}

func remoteLayoutTargetLock(store *RemoteHostStore, hostID string) *sync.Mutex {
	key := store.Path() + "\x00" + hostID
	lock, _ := remoteLayoutTargetLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// loadRemoteLayout returns an empty layout when the Host has never persisted
// one. Existing corrupt or unsafe state is surfaced instead of being silently
// replaced, so callers can preserve it for diagnosis/recovery.
func loadRemoteLayout(store *RemoteHostStore, hostID string) (remoteLayoutDocument, error) {
	if store == nil {
		return remoteLayoutDocument{}, errors.New("Remote Host store is unavailable")
	}
	lock := remoteLayoutTargetLock(store, hostID)
	lock.Lock()
	defer lock.Unlock()
	host, found, err := store.Get(hostID)
	if err != nil {
		return remoteLayoutDocument{}, err
	}
	if !found {
		return remoteLayoutDocument{}, fmt.Errorf("Remote Host entry %q is not saved", hostID)
	}
	if host.LayoutRef == "" {
		return newRemoteLayoutDocument(hostID), nil
	}
	path, err := remoteLayoutPath(store, host)
	if err != nil {
		return remoteLayoutDocument{}, err
	}
	return readRemoteLayoutFile(path, hostID)
}

func updateRemoteLayout(store *RemoteHostStore, hostID string, mutate func(*remoteLayoutDocument) error) error {
	if store == nil {
		return errors.New("Remote Host store is unavailable")
	}
	if mutate == nil {
		return errors.New("Remote layout mutation is required")
	}
	lock := remoteLayoutTargetLock(store, hostID)
	lock.Lock()
	defer lock.Unlock()
	host, found, err := store.Get(hostID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Remote Host entry %q is not saved", hostID)
	}
	expectedRef, err := remoteLayoutRefForHost(hostID)
	if err != nil {
		return err
	}
	if host.LayoutRef == "" {
		if err := store.UpdateLayoutRef(hostID, expectedRef); err != nil {
			return fmt.Errorf("persist Remote layout reference: %w", err)
		}
		host.LayoutRef = expectedRef
	}
	path, err := remoteLayoutPath(store, host)
	if err != nil {
		return err
	}
	document, err := readRemoteLayoutFile(path, hostID)
	if errors.Is(err, os.ErrNotExist) {
		document = newRemoteLayoutDocument(hostID)
	} else if err != nil {
		return err
	}
	if err := mutate(&document); err != nil {
		return err
	}
	if err := validateRemoteLayoutDocument(&document, hostID); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Remote Desktop layout: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > remoteLayoutMaxBytes {
		return fmt.Errorf("Remote Desktop layout exceeds %d bytes", remoteLayoutMaxBytes)
	}
	if err := fileutil.AtomicWriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write Remote Desktop layout: %w", err)
	}
	return nil
}

func readRemoteLayoutFile(path, hostID string) (remoteLayoutDocument, error) {
	file, info, err := openRemoteHostStoreFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return remoteLayoutDocument{}, os.ErrNotExist
		}
		if errors.Is(err, ErrRemoteHostStoreUnsafe) {
			return remoteLayoutDocument{}, fmt.Errorf("%w: %v", ErrRemoteLayoutUnsafe, err)
		}
		return remoteLayoutDocument{}, fmt.Errorf("open Remote Desktop layout: %w", err)
	}
	defer file.Close()
	if !info.Mode().IsRegular() {
		return remoteLayoutDocument{}, fmt.Errorf("%w: path is not a regular file", ErrRemoteLayoutUnsafe)
	}
	if err := validateRemoteHostStorePermissions(info); err != nil {
		return remoteLayoutDocument{}, fmt.Errorf("%w: %v", ErrRemoteLayoutUnsafe, err)
	}
	if info.Size() < 0 || info.Size() > remoteLayoutMaxBytes {
		return remoteLayoutDocument{}, fmt.Errorf("%w: document exceeds %d bytes", ErrRemoteLayoutCorrupt, remoteLayoutMaxBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(file, remoteLayoutMaxBytes+1))
	if err != nil {
		return remoteLayoutDocument{}, fmt.Errorf("read Remote Desktop layout: %w", err)
	}
	if len(raw) > remoteLayoutMaxBytes {
		return remoteLayoutDocument{}, fmt.Errorf("%w: document exceeds %d bytes", ErrRemoteLayoutCorrupt, remoteLayoutMaxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document remoteLayoutDocument
	if err := decoder.Decode(&document); err != nil {
		return remoteLayoutDocument{}, fmt.Errorf("%w: %v", ErrRemoteLayoutCorrupt, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return remoteLayoutDocument{}, fmt.Errorf("%w: %v", ErrRemoteLayoutCorrupt, err)
	}
	if err := validateRemoteLayoutDocument(&document, hostID); err != nil {
		if errors.Is(err, ErrRemoteLayoutUnsafe) {
			return remoteLayoutDocument{}, err
		}
		return remoteLayoutDocument{}, fmt.Errorf("%w: %v", ErrRemoteLayoutCorrupt, err)
	}
	return document, nil
}

func validateRemoteLayoutDocument(document *remoteLayoutDocument, hostID string) error {
	if document == nil {
		return errors.New("Remote layout document is required")
	}
	if document.Version != remoteLayoutVersion {
		return fmt.Errorf("unsupported version %d", document.Version)
	}
	if document.HostID != hostID {
		return fmt.Errorf("layout belongs to Host %q, not %q", document.HostID, hostID)
	}
	if err := ValidateRemoteHostEntryID(document.HostID); err != nil {
		return err
	}
	if len(document.WorkspaceTitles) > remoteLayoutMaxWorkspaces || len(document.WorkspaceColors) > remoteLayoutMaxWorkspaces || len(document.PinnedTopics) > remoteLayoutMaxWorkspaces {
		return fmt.Errorf("too many workspace preferences")
	}
	for workspaceID, title := range document.WorkspaceTitles {
		if err := validateRemoteLayoutOpaqueID("workspace", workspaceID); err != nil {
			return err
		}
		if title == "" || title != strings.TrimSpace(title) {
			return fmt.Errorf("workspace title for %q is not canonical", workspaceID)
		}
		if err := validateRemoteLayoutText("workspace title", title, remoteLayoutMaxTitleBytes); err != nil {
			return err
		}
	}
	for workspaceID, color := range document.WorkspaceColors {
		if err := validateRemoteLayoutOpaqueID("workspace", workspaceID); err != nil {
			return err
		}
		if color == "" || normalizeProjectColor(color) != color {
			return fmt.Errorf("workspace color for %q is invalid", workspaceID)
		}
	}
	if err := validateRemoteLayoutIDList("workspaceOrder", document.WorkspaceOrder, remoteLayoutMaxWorkspaces); err != nil {
		return err
	}
	if err := validateRemoteLayoutIDList("pinnedWorkspaces", document.PinnedWorkspaces, remoteLayoutMaxWorkspaces); err != nil {
		return err
	}
	topicCount := 0
	for workspaceID, topics := range document.PinnedTopics {
		if err := validateRemoteLayoutOpaqueID("workspace", workspaceID); err != nil {
			return err
		}
		topicCount += len(topics)
		if topicCount > remoteLayoutMaxPinnedTopics {
			return fmt.Errorf("too many pinned topic preferences")
		}
		if err := validateRemoteLayoutIDList("pinnedTopics", topics, remoteLayoutMaxPinnedTopics); err != nil {
			return err
		}
	}
	if document.WorkspaceTitles == nil {
		document.WorkspaceTitles = make(map[string]string)
	}
	if document.WorkspaceColors == nil {
		document.WorkspaceColors = make(map[string]string)
	}
	if document.PinnedTopics == nil {
		document.PinnedTopics = make(map[string][]string)
	}
	return nil
}

func validateRemoteLayoutIDList(field string, values []string, limit int) error {
	if len(values) > limit {
		return fmt.Errorf("%s exceeds %d entries", field, limit)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := validateRemoteLayoutOpaqueID(field, value); err != nil {
			return err
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s contains duplicate identity %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateRemoteLayoutOpaqueID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s identity is required", field)
	}
	if len(value) > remoteLayoutMaxOpaqueIDBytes || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s identity is invalid", field)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s identity contains a control character", field)
		}
	}
	return nil
}

func validateRemoteLayoutText(field, value string, maxBytes int) error {
	if len(value) > maxBytes || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s is invalid", field)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s contains a control character", field)
		}
	}
	return nil
}

func (a *App) remoteLayoutWorkspaceMutation(workspaceRoot string, mutate func(*remoteLayoutDocument, string)) error {
	workspaceID := runtimeapi.WorkspaceID(workspaceRoot)
	if err := validateRemoteLayoutOpaqueID("workspace", workspaceRoot); err != nil {
		return err
	}
	api, target, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	if _, err := remoteWorkspaceByID(ctx, api, workspaceID); err != nil {
		return err
	}
	return updateRemoteLayout(a.remote.store, target.ID, func(document *remoteLayoutDocument) error {
		mutate(document, workspaceRoot)
		return nil
	})
}

func (a *App) remoteRenameProject(workspaceRoot, title string) error {
	title = strings.TrimSpace(title)
	if title != "" {
		if err := validateRemoteLayoutText("workspace title", title, remoteLayoutMaxTitleBytes); err != nil {
			return err
		}
	}
	return a.remoteLayoutWorkspaceMutation(workspaceRoot, func(document *remoteLayoutDocument, workspaceID string) {
		if title == "" {
			delete(document.WorkspaceTitles, workspaceID)
		} else {
			document.WorkspaceTitles[workspaceID] = title
		}
	})
}

func (a *App) remoteSetProjectColor(workspaceRoot, color string) error {
	color = normalizeProjectColor(color)
	return a.remoteLayoutWorkspaceMutation(workspaceRoot, func(document *remoteLayoutDocument, workspaceID string) {
		if color == "" {
			delete(document.WorkspaceColors, workspaceID)
		} else {
			document.WorkspaceColors[workspaceID] = color
		}
	})
}

func (a *App) remoteSetProjectPinned(workspaceRoot string, pinned bool) error {
	return a.remoteLayoutWorkspaceMutation(workspaceRoot, func(document *remoteLayoutDocument, workspaceID string) {
		document.PinnedWorkspaces = removeRemoteLayoutID(document.PinnedWorkspaces, workspaceID)
		if pinned {
			document.PinnedWorkspaces = append([]string{workspaceID}, document.PinnedWorkspaces...)
		}
	})
}

func (a *App) remoteReorderProjects(workspaceRoots []string) error {
	api, target, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	workspaces, err := listAllRemoteWorkspaces(ctx, api)
	if err != nil {
		return err
	}
	known := make(map[string]struct{}, len(workspaces))
	for _, workspace := range workspaces {
		known[string(workspace.ID)] = struct{}{}
	}
	if len(workspaceRoots) != len(known) {
		return fmt.Errorf("Remote workspace order length mismatch")
	}
	seen := make(map[string]struct{}, len(workspaceRoots))
	for _, workspaceID := range workspaceRoots {
		if err := validateRemoteLayoutOpaqueID("workspace", workspaceID); err != nil {
			return err
		}
		if _, ok := known[workspaceID]; !ok {
			return fmt.Errorf("Remote workspace %q not found", workspaceID)
		}
		if _, duplicate := seen[workspaceID]; duplicate {
			return fmt.Errorf("duplicate Remote workspace %q", workspaceID)
		}
		seen[workspaceID] = struct{}{}
	}
	return updateRemoteLayout(a.remote.store, target.ID, func(document *remoteLayoutDocument) error {
		document.WorkspaceOrder = append([]string(nil), workspaceRoots...)
		return nil
	})
}

func (a *App) remoteSetTopicPinned(topicIDValue string, pinned bool) error {
	if err := validateRemoteLayoutOpaqueID("topic", topicIDValue); err != nil {
		return err
	}
	api, target, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	topicID := runtimeapi.TopicID(topicIDValue)
	workspaceID, err := a.remoteWorkspaceForTopic(ctx, api, topicID)
	if err != nil {
		return err
	}
	return updateRemoteLayout(a.remote.store, target.ID, func(document *remoteLayoutDocument) error {
		workspaceKey := string(workspaceID)
		topics := removeRemoteLayoutID(document.PinnedTopics[workspaceKey], topicIDValue)
		if pinned {
			topics = append([]string{topicIDValue}, topics...)
		}
		if len(topics) == 0 {
			delete(document.PinnedTopics, workspaceKey)
		} else {
			document.PinnedTopics[workspaceKey] = topics
		}
		return nil
	})
}

func removeRemoteLayoutID(values []string, target string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			out = append(out, value)
		}
	}
	return out
}

func applyRemoteProjectLayout(nodes []ProjectNode, document remoteLayoutDocument) []ProjectNode {
	pinnedWorkspaces := make(map[string]struct{}, len(document.PinnedWorkspaces))
	for _, workspaceID := range document.PinnedWorkspaces {
		pinnedWorkspaces[workspaceID] = struct{}{}
	}
	for index := range nodes {
		workspaceID := nodes[index].Root
		if title := document.WorkspaceTitles[workspaceID]; title != "" {
			nodes[index].Label = title
		}
		color := document.WorkspaceColors[workspaceID]
		nodes[index].ProjectColor = color
		_, nodes[index].Pinned = pinnedWorkspaces[workspaceID]
		pinnedTopics := document.PinnedTopics[workspaceID]
		pinnedTopicSet := make(map[string]struct{}, len(pinnedTopics))
		for _, topicID := range pinnedTopics {
			pinnedTopicSet[topicID] = struct{}{}
		}
		for topicIndex := range nodes[index].Children {
			topic := &nodes[index].Children[topicIndex]
			topic.ProjectColor = color
			_, topic.Pinned = pinnedTopicSet[topic.TopicID]
			for sessionIndex := range topic.Children {
				topic.Children[sessionIndex].ProjectColor = color
			}
		}
		nodes[index].Children = orderRemoteProjectNodes(nodes[index].Children, pinnedTopics, func(node ProjectNode) string { return node.TopicID })
	}
	nodes = orderRemoteProjectNodes(nodes, document.WorkspaceOrder, func(node ProjectNode) string { return node.Root })
	return orderRemoteProjectNodes(nodes, document.PinnedWorkspaces, func(node ProjectNode) string { return node.Root })
}

func orderRemoteProjectNodes(nodes []ProjectNode, order []string, identity func(ProjectNode) string) []ProjectNode {
	if len(order) == 0 || len(nodes) < 2 {
		return nodes
	}
	byID := make(map[string]ProjectNode, len(nodes))
	for _, node := range nodes {
		byID[identity(node)] = node
	}
	seen := make(map[string]struct{}, len(nodes))
	out := make([]ProjectNode, 0, len(nodes))
	for _, id := range order {
		node, ok := byID[id]
		if !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, node)
	}
	for _, node := range nodes {
		id := identity(node)
		if _, already := seen[id]; already {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, node)
	}
	return out
}
