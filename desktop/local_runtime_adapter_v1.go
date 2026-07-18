package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"

	"reasonix/internal/boot"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
	"reasonix/internal/skill"
)

var _ runtimeapi.V1RuntimeAPI = (*LocalTargetAdapter)(nil)

type localRuntimeOperation struct {
	id     runtimeapi.OperationID
	kind   runtimeapi.OperationKind
	handle *control.OperationHandle
}

type localRuntimeV1State struct {
	catalogEvents chan runtimeapi.CatalogInvalidation
	closed        bool
	revision      atomic.Uint64

	promptHistory    *runtimeservice.PromptHistoryService
	promptHistoryErr error
	fileGit          map[runtimeapi.SessionRef]localFileGitEntry
	memoryResearch   map[runtimeapi.SessionRef]localMemoryResearchEntry
	jobs             map[runtimeapi.JobID]localJobBinding
	records          map[runtimeapi.SessionRef]localSessionRecord
	directories      map[runtimeapi.DirectoryRef]string
	cursors          map[runtimeapi.Cursor]localRuntimeCursor
	closedWorkspaces map[runtimeapi.WorkspaceID]bool
	closedSessions   map[runtimeapi.SessionRef]bool
	trashedSessions  map[runtimeapi.SessionRef]bool
	purgedSessions   map[runtimeapi.SessionRef]bool
	checkpointByTurn map[runtimeapi.SessionRef]map[int]localCheckpointBinding
	checkpointIDs    map[runtimeapi.CheckpointID]localCheckpointTarget
}

type localFileGitEntry struct {
	incarnation string
	root        string
	service     *runtimeservice.FileGitService
}

type localMemoryResearchEntry struct {
	incarnation string
	root        string
	service     *runtimeservice.MemoryResearchService
}

type localSessionRecord struct {
	ref           runtimeapi.SessionRef
	path          string
	sessionDir    string
	workspaceRoot string
	scope         string
	topicID       string
	topicTitle    string
	title         string
	preview       string
	turns         int
	createdAt     int64
	lastActivity  int64
	deletedAt     int64
	recoveryCopy  bool
	trashed       bool
	tabID         string
}

type localJobBinding struct {
	session runtimeapi.SessionRef
	rawID   string
}

type localRuntimeCursor struct {
	kind     string
	binding  string
	revision string
	offset   int
}

type localCheckpointBinding struct {
	id          runtimeapi.CheckpointID
	fingerprint string
}

type localCheckpointTarget struct {
	session     runtimeapi.SessionRef
	turn        int
	fingerprint string
}

func newLocalRuntimeV1State() localRuntimeV1State {
	history, err := runtimeservice.NewPromptHistoryService(runtimeservice.PromptHistoryOptions{})
	return localRuntimeV1State{
		catalogEvents: make(chan runtimeapi.CatalogInvalidation, 128),
		promptHistory: history, promptHistoryErr: err,
		fileGit:          make(map[runtimeapi.SessionRef]localFileGitEntry),
		memoryResearch:   make(map[runtimeapi.SessionRef]localMemoryResearchEntry),
		jobs:             make(map[runtimeapi.JobID]localJobBinding),
		records:          make(map[runtimeapi.SessionRef]localSessionRecord),
		directories:      make(map[runtimeapi.DirectoryRef]string),
		cursors:          make(map[runtimeapi.Cursor]localRuntimeCursor),
		closedWorkspaces: make(map[runtimeapi.WorkspaceID]bool),
		closedSessions:   make(map[runtimeapi.SessionRef]bool),
		trashedSessions:  make(map[runtimeapi.SessionRef]bool),
		purgedSessions:   make(map[runtimeapi.SessionRef]bool),
		checkpointByTurn: make(map[runtimeapi.SessionRef]map[int]localCheckpointBinding),
		checkpointIDs:    make(map[runtimeapi.CheckpointID]localCheckpointTarget),
	}
}

func localCheckpointFingerprint(value CheckpointMeta) string {
	return localHash(struct {
		Turn            int
		Prompt          string
		Files           []string
		FileCount       int
		FilesTruncated  bool
		TurnFileCount   int
		Time            int64
		CanCode         bool
		CanConversation bool
	}{
		Turn: value.Turn, Prompt: value.Prompt, Files: append([]string(nil), value.Files...),
		FileCount: value.FileCount, FilesTruncated: value.FilesTruncated, TurnFileCount: value.TurnFileCount,
		Time: value.Time, CanCode: value.CanCode, CanConversation: value.CanConversation,
	})
}

func (a *LocalTargetAdapter) syncLocalCheckpointsLocked(ref runtimeapi.SessionRef, values []CheckpointMeta) (map[int]runtimeapi.CheckpointID, error) {
	byTurn := a.v1.checkpointByTurn[ref]
	if byTurn == nil {
		byTurn = make(map[int]localCheckpointBinding)
		a.v1.checkpointByTurn[ref] = byTurn
	}
	fingerprints := make(map[int]string, len(values))
	for _, value := range values {
		fingerprints[value.Turn] = localCheckpointFingerprint(value)
	}
	for turn, binding := range byTurn {
		if fingerprint, ok := fingerprints[turn]; !ok || fingerprint != binding.fingerprint {
			delete(a.v1.checkpointIDs, binding.id)
			delete(byTurn, turn)
		}
	}
	result := make(map[int]runtimeapi.CheckpointID, len(values))
	for _, value := range values {
		fingerprint := fingerprints[value.Turn]
		binding, ok := byTurn[value.Turn]
		if !ok {
			raw, err := newLocalOpaqueID("local_checkpoint")
			if err != nil {
				return nil, err
			}
			binding = localCheckpointBinding{id: runtimeapi.CheckpointID(raw), fingerprint: fingerprint}
			byTurn[value.Turn] = binding
		}
		a.v1.checkpointIDs[binding.id] = localCheckpointTarget{session: ref, turn: value.Turn, fingerprint: fingerprint}
		result[value.Turn] = binding.id
	}
	return result, nil
}

func (a *LocalTargetAdapter) invalidateLocalCheckpointsLocked(ref runtimeapi.SessionRef) {
	for _, binding := range a.v1.checkpointByTurn[ref] {
		delete(a.v1.checkpointIDs, binding.id)
	}
	delete(a.v1.checkpointByTurn, ref)
}

func (s *localRuntimeV1State) close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	close(s.catalogEvents)
}

func localContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func localCheckContext(ctx context.Context) error {
	return localContext(ctx).Err()
}

func (a *LocalTargetAdapter) admitLocalCall(ctx context.Context) (func(), error) {
	if err := localCheckContext(ctx); err != nil {
		return nil, err
	}
	if err := a.beginAppAdmission(); err != nil {
		return nil, err
	}
	return a.app.endLocalTargetAdmission, nil
}

func localWorkspaceIDForRoot(scope, root string) runtimeapi.WorkspaceID {
	scope = strings.TrimSpace(scope)
	if scope == "global" {
		return runtimeapi.WorkspaceID(localOpaqueID("local_workspace", "global"))
	}
	root = normalizeProjectRoot(root)
	return runtimeapi.WorkspaceID(localOpaqueID("local_workspace", "project\x00"+root))
}

func localWorkspaceID(tab *WorkspaceTab) runtimeapi.WorkspaceID {
	if tab == nil {
		return ""
	}
	return localWorkspaceIDForRoot(tab.Scope, tab.WorkspaceRoot)
}

func localSessionIDForPath(workspaceID runtimeapi.WorkspaceID, path, fallback string) runtimeapi.SessionID {
	identity := sessionRuntimeKey(path)
	if identity == "" {
		identity = "transient:" + strings.TrimSpace(fallback)
	}
	return runtimeapi.SessionID(localOpaqueID("local_session", string(workspaceID)+"\x00"+identity))
}

func localSessionID(tab *WorkspaceTab) runtimeapi.SessionID {
	if tab == nil {
		return ""
	}
	workspaceID := localWorkspaceID(tab)
	return localSessionIDForPath(workspaceID, tab.currentSessionPath(), tab.ID)
}

func localTopicID(workspaceID runtimeapi.WorkspaceID, raw string) runtimeapi.TopicID {
	return runtimeapi.TopicID(localOpaqueID("local_topic", string(workspaceID)+"\x00"+strings.TrimSpace(raw)))
}

func localRuntimeIncarnation(a *LocalTargetAdapter, record localSessionRecord) string {
	generation := uint64(0)
	if a != nil && a.app != nil {
		// Every caller reaches this helper through withLocalSession and therefore
		// already holds the Local admission read barrier. Taking a recursive
		// RLock here could deadlock behind a pending detach writer.
		generation = a.app.localTarget.generation
	}
	return fmt.Sprintf("%d:%s", generation, sessionRuntimeKey(record.path))
}

func localHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (a *LocalTargetAdapter) CatalogEvents() <-chan runtimeapi.CatalogInvalidation {
	return a.v1.catalogEvents
}

func (a *LocalTargetAdapter) notifyLocalCatalog(scope runtimeapi.CatalogScope, workspaceIDs []runtimeapi.WorkspaceID, kinds ...runtimeapi.CatalogKind) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.v1.closed {
		return
	}
	revision := a.v1.revision.Add(1)
	change := runtimeapi.CatalogInvalidation{
		Revision: runtimeapi.CatalogRevision(fmt.Sprintf("local_catalog_%d", revision)), Scope: scope,
		AffectedWorkspaceIDs: append([]runtimeapi.WorkspaceID(nil), workspaceIDs...),
		Kinds:                append([]runtimeapi.CatalogKind(nil), kinds...),
	}
	select {
	case a.v1.catalogEvents <- change:
	default:
		// Invalidations are hints. A saturated observer re-queries on the next
		// successfully delivered revision; Local state itself is authoritative.
	}
}

func (a *LocalTargetAdapter) withLocalSession(ctx context.Context, ref runtimeapi.SessionRef) (*WorkspaceTab, control.SessionAPI, localSessionRecord, error) {
	if err := localCheckContext(ctx); err != nil {
		return nil, nil, localSessionRecord{}, err
	}
	if err := a.beginAppAdmission(); err != nil {
		return nil, nil, localSessionRecord{}, err
	}
	a.mu.Lock()
	state, tab, err := a.sessionLocked(ref)
	if err == nil {
		a.app.mu.RLock()
		ctrl := tab.Ctrl
		a.app.mu.RUnlock()
		if ctrl == nil || !tab.Ready {
			err = a.app.workspaceNotReadyErr(tab)
		} else {
			record := a.recordForLiveSessionLocked(state, tab)
			a.mu.Unlock()
			return tab, ctrl, record, nil
		}
	}
	a.mu.Unlock()
	a.app.endLocalTargetAdmission()
	return nil, nil, localSessionRecord{}, err
}

func (a *LocalTargetAdapter) endLocalSession() { a.app.endLocalTargetAdmission() }

func (a *LocalTargetAdapter) recordForLiveSessionLocked(state *localRuntimeSession, tab *WorkspaceTab) localSessionRecord {
	record := localSessionRecord{
		ref: state.ref, path: tab.currentSessionPath(), sessionDir: tabRuntimeSessionDir(tab),
		workspaceRoot: tab.WorkspaceRoot, scope: tab.Scope, topicID: tab.TopicID,
		topicTitle: tab.TopicTitle, title: tab.TopicTitle, tabID: tab.ID,
	}
	if record.scope == "global" {
		record.workspaceRoot = globalWorkspaceRoot()
	}
	a.v1.records[state.ref] = record
	return record
}

func (a *LocalTargetAdapter) resolveWorkspace(id runtimeapi.WorkspaceID) (string, string, error) {
	if strings.TrimSpace(string(id)) == "" {
		return "", "", errors.New("workspaceId is required")
	}
	for _, item := range a.app.ListWorkspaces() {
		if localWorkspaceIDForRoot("project", item.Path) == id {
			return "project", normalizeProjectRoot(item.Path), nil
		}
	}
	a.app.mu.RLock()
	defer a.app.mu.RUnlock()
	for _, tab := range a.app.tabs {
		if tab != nil && localWorkspaceID(tab) == id {
			if tab.Scope == "global" {
				return "global", globalWorkspaceRoot(), nil
			}
			return "project", normalizeProjectRoot(tab.WorkspaceRoot), nil
		}
	}
	return "", "", errors.New("Local workspace is unknown")
}

func (a *LocalTargetAdapter) HostCapabilities(ctx context.Context) (runtimeapi.Capabilities, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.Capabilities{}, err
	}
	return localRuntimeCapabilities(), nil
}

func (a *LocalTargetAdapter) HostConfigSummary(ctx context.Context) (runtimeapi.HostConfigSummary, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.HostConfigSummary{}, err
	}
	models := a.app.Models()
	modelRefs := make([]string, 0, len(models))
	defaultModel := ""
	for _, model := range models {
		modelRefs = append(modelRefs, model.Ref)
		if model.Current {
			defaultModel = model.Ref
		}
	}
	sort.Strings(modelRefs)
	result := runtimeapi.HostConfigSummary{
		Available: true, DefaultModel: defaultModel, Models: modelRefs,
		CollaborationModes: []string{"normal", "plan", "goal"},
		TokenModes:         []string{boot.TokenModeFull, boot.TokenModeEconomy, boot.TokenModeDelivery},
		ToolApprovalModes:  []string{control.ToolApprovalAsk, control.ToolApprovalAuto, control.ToolApprovalDontAsk, control.ToolApprovalYolo},
		EffectiveScopes:    []runtimeapi.EffectiveScope{{Name: "built-in", Active: true}, {Name: "user", Active: true}, {Name: "workspace", Active: true}},
		DisplayPaths:       []runtimeapi.ConfigDisplayPath{{Scope: "user", DisplayPath: "<reasonix-home>/config.toml"}, {Scope: "workspace", DisplayPath: "<workspace>/reasonix.toml"}},
		FeatureStates:      []runtimeapi.FeatureState{{Feature: "memory", Available: true}, {Feature: "research", Available: true}},
		CLIHints:           []runtimeapi.CLIHint{{Label: "Configure Local runtime", Command: "reasonix setup"}},
	}
	result.Revision = runtimeapi.CatalogRevision("local_config_" + localHash(result))
	return result, nil
}

func (a *LocalTargetAdapter) Connection(ctx context.Context) (runtimeapi.ConnectionView, error) {
	configSummary, err := a.HostConfigSummary(ctx)
	if err != nil {
		return runtimeapi.ConnectionView{}, err
	}
	return runtimeapi.ConnectionView{
		Label: "This computer", OS: runtime.GOOS, Arch: runtime.GOARCH,
		Capabilities: localRuntimeCapabilities(), Config: configSummary,
	}, nil
}

func normalizedLocalLimit(limit int) (int, error) {
	if err := runtimeapi.ValidatePageLimit(limit); err != nil {
		return 0, err
	}
	if limit == 0 {
		return runtimeapi.PageDefaultItems, nil
	}
	return limit, nil
}

func (a *LocalTargetAdapter) localPage(kind, binding, revision string, cursor runtimeapi.Cursor, limit, total int) (int, int, runtimeapi.Cursor, bool, error) {
	pageLimit, err := normalizedLocalLimit(limit)
	if err != nil {
		return 0, 0, "", false, err
	}
	offset := 0
	a.mu.Lock()
	defer a.mu.Unlock()
	if cursor != "" {
		decoded, ok := a.v1.cursors[cursor]
		if !ok || decoded.kind != kind || decoded.binding != binding || decoded.revision != revision || decoded.offset < 0 || decoded.offset > total {
			return 0, 0, "", false, runtimeservice.ErrStaleCursor
		}
		offset = decoded.offset
	}
	end := offset + pageLimit
	if end > total {
		end = total
	}
	hasMore := end < total
	var next runtimeapi.Cursor
	if hasMore {
		id, idErr := newLocalOpaqueID("local_cursor")
		if idErr != nil {
			return 0, 0, "", false, idErr
		}
		next = runtimeapi.Cursor(id)
		a.v1.cursors[next] = localRuntimeCursor{kind: kind, binding: binding, revision: revision, offset: end}
	}
	return offset, end, next, hasMore, nil
}

func (a *LocalTargetAdapter) BrowseWorkspace(ctx context.Context, input runtimeapi.BrowseWorkspaceInput) (runtimeapi.WorkspacePage, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.WorkspacePage{}, err
	}
	if input.DirectoryRef != "" && strings.TrimSpace(input.TypedPath) != "" {
		return runtimeapi.WorkspacePage{}, errors.New("directoryRef and typedPath are mutually exclusive")
	}
	path := strings.TrimSpace(input.TypedPath)
	a.mu.Lock()
	if input.DirectoryRef != "" {
		path = a.v1.directories[input.DirectoryRef]
	}
	a.mu.Unlock()
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = home
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return runtimeapi.WorkspacePage{}, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return runtimeapi.WorkspacePage{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("workspace browser target is not a directory")
		}
		return runtimeapi.WorkspacePage{}, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return runtimeapi.WorkspacePage{}, err
	}
	directories := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(resolved, entry.Name()))
		}
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.ToLower(filepath.Base(directories[i])) < strings.ToLower(filepath.Base(directories[j]))
	})
	revision := localHash(directories)
	start, end, next, more, err := a.localPage("workspace/browse", resolved, revision, input.Cursor, input.Limit, len(directories))
	if err != nil {
		return runtimeapi.WorkspacePage{}, err
	}
	toDirectory := func(path string) runtimeapi.Directory {
		ref := runtimeapi.DirectoryRef(localOpaqueID("local_directory", path))
		parent := filepath.Dir(path)
		parentRef := runtimeapi.DirectoryRef("")
		if parent != path {
			parentRef = runtimeapi.DirectoryRef(localOpaqueID("local_directory", parent))
		}
		a.mu.Lock()
		a.v1.directories[ref] = path
		if parentRef != "" {
			a.v1.directories[parentRef] = parent
		}
		a.mu.Unlock()
		return runtimeapi.Directory{Ref: ref, Name: filepath.Base(path), DisplayPath: path, ParentRef: parentRef}
	}
	items := make([]runtimeapi.Directory, 0, end-start)
	for _, item := range directories[start:end] {
		items = append(items, toDirectory(item))
	}
	return runtimeapi.WorkspacePage{Directory: toDirectory(resolved), Entries: items, HasMore: more, Next: next}, nil
}

func (a *LocalTargetAdapter) OpenWorkspace(ctx context.Context, input runtimeapi.OpenWorkspaceInput) (runtimeapi.OpenWorkspaceResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.OpenWorkspaceResult{}, err
	}
	defer release()
	a.mu.Lock()
	root := a.v1.directories[input.PrimaryDirectory]
	a.mu.Unlock()
	if root == "" {
		return runtimeapi.OpenWorkspaceResult{}, errors.New("Local directory reference is unknown")
	}
	already := false
	for _, workspace := range a.app.ListWorkspaces() {
		if sameProjectRoot(workspace.Path, root) {
			already = true
			break
		}
	}
	if !already {
		if err := addProject(root, ""); err != nil {
			return runtimeapi.OpenWorkspaceResult{}, err
		}
		a.app.syncTabWorkspaceRootSpellings()
		a.app.emitProjectTreeChanged()
		a.notifyLocalCatalog(runtimeapi.CatalogHost, nil, runtimeapi.CatalogWorkspaceCatalog)
	}
	workspaceID := localWorkspaceIDForRoot("project", root)
	a.mu.Lock()
	delete(a.v1.closedWorkspaces, workspaceID)
	a.mu.Unlock()
	return runtimeapi.OpenWorkspaceResult{Workspace: runtimeapi.Workspace{
		ID: workspaceID, Name: workspaceName(root), DisplayPath: root,
	}, AlreadyOpen: already}, nil
}

func (a *LocalTargetAdapter) ListWorkspaces(ctx context.Context, input runtimeapi.ListWorkspacesInput) (runtimeapi.WorkspaceListPage, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.WorkspaceListPage{}, err
	}
	values := a.app.ListWorkspaces()
	items := make([]runtimeapi.Workspace, 0, len(values))
	for _, value := range values {
		root := normalizeProjectRoot(value.Path)
		items = append(items, runtimeapi.Workspace{ID: localWorkspaceIDForRoot("project", root), Name: value.Name, DisplayPath: root})
	}
	revision := localHash(items)
	start, end, next, more, err := a.localPage("workspace/list", "host", revision, input.Cursor, input.Limit, len(items))
	if err != nil {
		return runtimeapi.WorkspaceListPage{}, err
	}
	return runtimeapi.WorkspaceListPage{Items: append([]runtimeapi.Workspace(nil), items[start:end]...), HasMore: more, Next: next}, nil
}

func (a *LocalTargetAdapter) CloseWorkspace(ctx context.Context, input runtimeapi.CloseWorkspaceInput) (runtimeapi.CloseWorkspaceResult, error) {
	release, err := a.admitLocalCall(ctx)
	if err != nil {
		return runtimeapi.CloseWorkspaceResult{}, err
	}
	defer release()
	a.mu.Lock()
	alreadyClosed := a.v1.closedWorkspaces[input.WorkspaceID]
	a.mu.Unlock()
	if alreadyClosed {
		return runtimeapi.CloseWorkspaceResult{Disposition: runtimeapi.WorkspaceAlreadyClosed}, nil
	}
	_, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.CloseWorkspaceResult{}, err
	}
	if err := a.app.RemoveWorkspace(root); err != nil {
		return runtimeapi.CloseWorkspaceResult{}, err
	}
	a.mu.Lock()
	a.v1.closedWorkspaces[input.WorkspaceID] = true
	a.mu.Unlock()
	a.notifyLocalCatalog(runtimeapi.CatalogHost, nil, runtimeapi.CatalogWorkspaceCatalog)
	return runtimeapi.CloseWorkspaceResult{Disposition: runtimeapi.WorkspaceClosed}, nil
}

func (a *LocalTargetAdapter) WorkspaceCatalog(ctx context.Context, input runtimeapi.WorkspaceCatalogInput) (runtimeapi.WorkspaceCatalog, error) {
	if err := localCheckContext(ctx); err != nil {
		return runtimeapi.WorkspaceCatalog{}, err
	}
	_, root, err := a.resolveWorkspace(input.WorkspaceID)
	if err != nil {
		return runtimeapi.WorkspaceCatalog{}, err
	}
	models := []ModelInfo{}
	var tab *WorkspaceTab
	a.app.mu.RLock()
	for _, candidate := range a.app.tabs {
		if candidate != nil && candidate.Scope == "project" && sameProjectRoot(candidate.WorkspaceRoot, root) {
			tab = candidate
			break
		}
	}
	a.app.mu.RUnlock()
	if tab != nil {
		models = a.app.ModelsForTab(tab.ID)
	} else {
		cfg, loadErr := config.LoadForRoot(root)
		if loadErr != nil {
			return runtimeapi.WorkspaceCatalog{}, loadErr
		}
		for index := range cfg.Providers {
			provider := &cfg.Providers[index]
			if !provider.Configured() {
				continue
			}
			for _, model := range provider.ChatModelList() {
				models = append(models, ModelInfo{Ref: provider.Name + "/" + model, Provider: provider.Name, Model: model})
			}
		}
	}
	items := make([]runtimeapi.ModelCatalogItem, 0, len(models))
	defaultProfile := runtimeapi.ResolvedProfile{CollaborationMode: "normal", TokenMode: boot.TokenModeFull, ToolApprovalMode: control.ToolApprovalAsk}
	for _, model := range models {
		effort := runtimeapi.EffortCatalog{Levels: []string{}}
		if tab != nil && model.Current {
			view := a.app.EffortForTab(tab.ID)
			effort = runtimeapi.EffortCatalog{Supported: view.Supported, Default: view.Default, Levels: append([]string(nil), view.Levels...)}
			defaultProfile = runtimeapi.ResolvedProfile{Model: model.Ref, Effort: view.Current, CollaborationMode: currentTabCollaborationMode(tab), TokenMode: currentTabTokenMode(tab), ToolApprovalMode: currentTabToolApprovalMode(tab)}
		}
		items = append(items, runtimeapi.ModelCatalogItem{Ref: runtimeapi.ModelRef(model.Ref), Provider: model.Provider, Model: model.Model, Effort: effort})
	}
	if defaultProfile.Model == "" && len(items) > 0 {
		defaultProfile.Model = string(items[0].Ref)
	}
	result := runtimeapi.WorkspaceCatalog{
		Models: items, CollaborationModes: []string{"normal", "plan", "goal"},
		TokenModes:        []string{boot.TokenModeFull, boot.TokenModeEconomy, boot.TokenModeDelivery},
		ToolApprovalModes: []string{control.ToolApprovalAsk, control.ToolApprovalAuto, control.ToolApprovalDontAsk, control.ToolApprovalYolo},
		DefaultProfile:    defaultProfile,
	}
	result.Revision = runtimeapi.CatalogRevision("local_workspace_catalog_" + localHash(result))
	return result, nil
}

func localSessionCatalogSource(ctrl control.SessionAPI) (runtimeservice.SessionCatalogSource, error) {
	if ctrl == nil {
		return runtimeservice.SessionCatalogSource{}, ErrLocalSessionUnknown
	}
	source := runtimeservice.SessionCatalogSource{
		CustomCommands: append([]command.Command(nil), ctrl.Commands()...),
		Skills:         append([]skill.Skill(nil), ctrl.SlashSkills()...),
	}
	configured := append([]string(nil), ctrl.ConfiguredMCPNames()...)
	disconnected := append([]string(nil), ctrl.DisconnectedMCPNames()...)
	byName := make(map[string]runtimeservice.CatalogMCPSource)
	for _, name := range append(configured, disconnected...) {
		if name = strings.TrimSpace(name); name != "" {
			byName[name] = runtimeservice.CatalogMCPSource{Name: name}
		}
	}
	if host := ctrl.Host(); host != nil {
		for _, server := range host.Servers() {
			byName[server.Name] = runtimeservice.CatalogMCPSource{Name: server.Name, Available: true, ToolCount: server.Tools}
		}
		for _, prompt := range host.Prompts() {
			source.AdditionalCommands = append(source.AdditionalCommands, runtimeservice.CatalogCommandSource{Name: prompt.Name, Description: prompt.Description})
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source.MCPServers = append(source.MCPServers, byName[name])
	}
	state, err := pluginpkg.LoadState(config.ReasonixHomeDir())
	if err != nil {
		return runtimeservice.SessionCatalogSource{}, err
	}
	source.Plugins = append(source.Plugins, state.Plugins...)
	return source, nil
}

func (a *LocalTargetAdapter) SessionCatalog(ctx context.Context, input runtimeapi.SessionCatalogInput) (runtimeapi.SessionCatalog, error) {
	_, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	defer a.endLocalSession()
	source, err := localSessionCatalogSource(ctrl)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	return runtimeservice.ProjectSessionCatalog(source)
}
