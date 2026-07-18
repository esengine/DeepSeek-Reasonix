package catalog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/store"
)

// ResolvedSession is the only filesystem-bearing target representation handed
// to Host runtime construction. Wire DTOs never contain these paths.
type ResolvedSession struct {
	Target          protocol.RuntimeTarget
	WorkspaceRoot   string
	AdditionalDirs  []string
	SessionDir      string
	SessionPath     string
	ResolvedProfile protocol.ResolvedProfile
}

// LoadSession reads the already-validated persistent transcript. A freshly
// created zero-byte transcript is a legitimate empty Session; a missing path is
// never silently replaced because that would bind the target to new history.
func (r ResolvedSession) LoadSession() (*agent.Session, error) {
	info, err := os.Lstat(r.SessionPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("session artifact is not a regular file")
	}
	return agent.LoadSession(r.SessionPath)
}

type SessionMetadata struct {
	Target              protocol.RuntimeTarget
	TopicID             protocol.TopicID
	TopicTitle          string
	Title               string
	Preview             string
	Turns               int
	CreatedAt           time.Time
	LastActivityAt      time.Time
	RecoveryInterrupted bool
	ResolvedProfile     protocol.ResolvedProfile
}

// CreatedSession is the durable half of session/create. RuntimeManager starts
// the incarnation and supplies RuntimeEpoch only after the record exists.
type CreatedSession struct {
	ResolvedSession
	TopicID    protocol.TopicID
	TopicTitle string
}

func (s CreatedSession) Result(runtimeEpoch protocol.RuntimeEpoch) protocol.SessionCreateResult {
	return protocol.SessionCreateResult{
		Target:          s.Target,
		RuntimeEpoch:    runtimeEpoch,
		TopicID:         s.TopicID,
		TopicTitle:      s.TopicTitle,
		ResolvedProfile: s.ResolvedProfile,
	}
}

func (c *Catalog) CreateSession(ctx context.Context, params protocol.SessionCreateParams) (CreatedSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return CreatedSession{}, err
	}
	if err := ctx.Err(); err != nil {
		return CreatedSession{}, err
	}
	workspace, ok := c.state.Workspaces[params.WorkspaceID]
	if !ok || !workspace.Open {
		return CreatedSession{}, catalogError(protocol.ErrWorkspaceNotFound, errors.New("workspace is not open"))
	}
	if err := c.syncWorkspaceSessionsLocked(ctx, workspace); err != nil {
		return CreatedSession{}, err
	}
	additional, err := c.resolveAdditionalDirectoriesLocked(workspace.CanonicalPath, params.AdditionalDirectoryRefs)
	if err != nil {
		return CreatedSession{}, err
	}
	profile, err := c.resolveCompleteProfileLocked(ctx, workspace.CanonicalPath, params.Profile)
	if err != nil {
		return CreatedSession{}, err
	}

	topicID, topicTitle, topicRecordValue, err := c.resolveCreateTopicLocked(params.WorkspaceID, params.Topic)
	if err != nil {
		return CreatedSession{}, err
	}
	sessionIDRaw, err := c.nextIDLocked("session")
	if err != nil {
		return CreatedSession{}, err
	}
	sessionID := protocol.SessionID(sessionIDRaw)
	target := protocol.RuntimeTarget{WorkspaceID: params.WorkspaceID, SessionID: sessionID}
	sessionDir, err := c.sessionStoreDirectoryLocked(workspace, true)
	if err != nil {
		return CreatedSession{}, err
	}
	sessionPath, err := createEmptySessionFile(sessionDir, profile.Model)
	if err != nil {
		return CreatedSession{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	cleanup := func() {
		_ = removeSessionArtifacts(sessionPath)
	}
	now := c.now().UTC()
	meta := agent.BranchMeta{
		RemoteSessionID:      string(sessionID),
		CreatedAt:            now,
		UpdatedAt:            now,
		Scope:                "project",
		WorkspaceRoot:        workspace.CanonicalPath,
		TopicID:              string(topicID),
		TopicTitle:           topicTitle,
		Model:                profile.Model,
		Effort:               profile.Effort,
		Mode:                 string(profile.CollaborationMode),
		TokenMode:            string(profile.TokenMode),
		ToolApprovalMode:     string(profile.ToolApprovalMode),
		AdditionalDirs:       append([]string{}, additional...),
		RemoteProfileVersion: 1,
		SchemaVersion:        agent.BranchMetaCountsVersion,
		Turns:                0,
		Preview:              "",
	}
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, meta); err != nil {
		cleanup()
		return CreatedSession{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	record := sessionRecord{ID: sessionID, WorkspaceID: params.WorkspaceID, Path: sessionPath, TopicID: topicID}
	if err := c.mutateLocked(func() error {
		if topicRecordValue != nil {
			if c.state.Topics[params.WorkspaceID] == nil {
				c.state.Topics[params.WorkspaceID] = make(map[protocol.TopicID]topicRecord)
			}
			c.state.Topics[params.WorkspaceID][topicID] = *topicRecordValue
		}
		c.state.Sessions[sessionID] = record
		return nil
	}); err != nil {
		cleanup()
		return CreatedSession{}, err
	}
	return CreatedSession{
		ResolvedSession: ResolvedSession{
			Target:          target,
			WorkspaceRoot:   workspace.CanonicalPath,
			AdditionalDirs:  append([]string(nil), additional...),
			SessionDir:      sessionDir,
			SessionPath:     sessionPath,
			ResolvedProfile: profile,
		},
		TopicID:    topicID,
		TopicTitle: topicTitle,
	}, nil
}

func createEmptySessionFile(dir, model string) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		path := agent.NewSessionPath(dir, model)
		if attempt > 0 {
			stem := strings.TrimSuffix(path, ".jsonl")
			path = fmt.Sprintf("%s-%02d.jsonl", stem, attempt)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return "", closeErr
			}
			return path, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", errors.New("could not allocate a unique session artifact")
}

func (c *Catalog) resolveCreateTopicLocked(workspaceID protocol.WorkspaceID, selection protocol.TopicSelection) (protocol.TopicID, string, *topicRecord, error) {
	topics := c.state.Topics[workspaceID]
	switch selection.Kind {
	case protocol.TopicExisting:
		topic, ok := topics[selection.TopicID]
		if !ok || topic.Trashed {
			return "", "", nil, catalogError(protocol.ErrTopicNotFound, errors.New("unknown Topic identity"))
		}
		return topic.ID, topic.Title, nil, nil
	case protocol.TopicNew:
		id, err := c.nextIDLocked("topic")
		if err != nil {
			return "", "", nil, err
		}
		title := strings.TrimSpace(selection.Title)
		if title == "" {
			title = c.defaultTopicTitle
		}
		record := topicRecord{ID: protocol.TopicID(id), Title: title, CreatedAtMs: c.now().UTC().UnixMilli()}
		return record.ID, record.Title, &record, nil
	default:
		return "", "", nil, catalogError(protocol.ErrTopicNotFound, errors.New("invalid Topic selection"))
	}
}

func (c *Catalog) resolveAdditionalDirectoriesLocked(primary string, refs []protocol.DirectoryRef) ([]string, error) {
	seen := map[string]bool{pathKey(primary): true}
	resolved := make([]string, 0, len(refs))
	for _, ref := range refs {
		path, ok := c.directoryRefs[ref]
		if !ok {
			return nil, catalogError(protocol.ErrStaleDirectoryRef, errors.New("unknown additional directory reference"))
		}
		canonical, err := canonicalExistingDirectory(path)
		if err != nil {
			return nil, err
		}
		key := pathKey(canonical)
		if seen[key] {
			continue
		}
		seen[key] = true
		resolved = append(resolved, canonical)
	}
	return resolved, nil
}

func (c *Catalog) resolveCompleteProfileLocked(ctx context.Context, workspaceRoot string, selection protocol.ProfileSelection) (protocol.ResolvedProfile, error) {
	if c.profileResolver == nil {
		return protocol.ResolvedProfile{}, catalogError(protocol.ErrInvalidProfile, errors.New("Host profile resolver is unavailable"))
	}
	profile, err := c.profileResolver.ResolveProfile(ctx, workspaceRoot, selection)
	if err != nil {
		if code, ok := ErrorCode(err); ok {
			return protocol.ResolvedProfile{}, catalogError(code, err)
		}
		return protocol.ResolvedProfile{}, catalogError(protocol.ErrInvalidProfile, err)
	}
	if err := validateResolvedProfile(profile); err != nil {
		return protocol.ResolvedProfile{}, catalogError(protocol.ErrInvalidProfile, err)
	}
	return profile, nil
}

func validateResolvedProfile(profile protocol.ResolvedProfile) error {
	if strings.TrimSpace(profile.Model) == "" || strings.TrimSpace(profile.Effort) == "" {
		return errors.New("resolved profile requires model and effort")
	}
	switch profile.CollaborationMode {
	case protocol.CollaborationNormal, protocol.CollaborationPlan, protocol.CollaborationGoal:
	default:
		return errors.New("resolved profile has invalid collaboration mode")
	}
	switch profile.TokenMode {
	case protocol.TokenFull, protocol.TokenEconomy, protocol.TokenDelivery:
	default:
		return errors.New("resolved profile has invalid token mode")
	}
	switch profile.ToolApprovalMode {
	case protocol.ToolApprovalAsk, protocol.ToolApprovalAuto, protocol.ToolApprovalYOLO:
	default:
		return errors.New("resolved profile has invalid tool approval mode")
	}
	return nil
}

func (c *Catalog) ResolveRuntimeTarget(ctx context.Context, target protocol.RuntimeTarget) (ResolvedSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resolveRuntimeTargetLocked(ctx, target)
}

func (c *Catalog) resolveRuntimeTargetLocked(ctx context.Context, target protocol.RuntimeTarget) (ResolvedSession, error) {
	if err := ctx.Err(); err != nil {
		return ResolvedSession{}, err
	}
	workspace, ok := c.state.Workspaces[target.WorkspaceID]
	if !ok || !workspace.Open {
		return ResolvedSession{}, catalogError(protocol.ErrWorkspaceNotFound, errors.New("workspace is not open"))
	}
	if err := c.syncWorkspaceSessionsLocked(ctx, workspace); err != nil {
		return ResolvedSession{}, err
	}
	record, ok := c.state.Sessions[target.SessionID]
	if !ok {
		return ResolvedSession{}, catalogError(protocol.ErrSessionNotFound, errors.New("unknown Session identity"))
	}
	if record.WorkspaceID != target.WorkspaceID {
		return ResolvedSession{}, catalogError(protocol.ErrWorkspaceSessionMismatch, errors.New("Session belongs to another workspace"))
	}
	if record.TrashPath != "" {
		return ResolvedSession{}, catalogError(protocol.ErrSessionTrashed, errors.New("Session is in trash"))
	}
	meta, err := c.validateLiveSessionRecordLocked(workspace, record)
	if err != nil {
		return ResolvedSession{}, err
	}
	profile := profileFromMeta(meta)
	if err := validateResolvedProfile(profile); err != nil || meta.RemoteProfileVersion < 1 {
		return ResolvedSession{}, catalogError(protocol.ErrInvalidProfile, errors.New("Session does not contain a complete resolved Host profile"))
	}
	additional, err := canonicalizeAdditionalDirectories(workspace.CanonicalPath, meta.AdditionalDirs)
	if err != nil {
		return ResolvedSession{}, err
	}
	return ResolvedSession{
		Target:          target,
		WorkspaceRoot:   workspace.CanonicalPath,
		AdditionalDirs:  additional,
		SessionDir:      filepath.Dir(record.Path),
		SessionPath:     record.Path,
		ResolvedProfile: profile,
	}, nil
}

func (c *Catalog) Metadata(ctx context.Context, target protocol.RuntimeTarget) (SessionMetadata, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	resolved, err := c.resolveRuntimeTargetLocked(ctx, target)
	if err != nil {
		return SessionMetadata{}, err
	}
	meta, ok, err := agent.LoadBranchMeta(resolved.SessionPath)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("Session metadata is missing")
		}
		return SessionMetadata{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	topic := c.state.Topics[target.WorkspaceID][protocol.TopicID(meta.TopicID)]
	created := meta.CreatedAt
	activity := meta.UpdatedAt
	if created.IsZero() {
		created = c.now().UTC()
	}
	if activity.IsZero() {
		activity = created
	}
	return SessionMetadata{
		Target:              target,
		TopicID:             topic.ID,
		TopicTitle:          topic.Title,
		Title:               sessionTitle(meta, topic.Title),
		Preview:             meta.Preview,
		Turns:               maxInt(meta.Turns, 0),
		CreatedAt:           created,
		LastActivityAt:      activity,
		RecoveryInterrupted: meta.Recovered || meta.InFlightTurn != nil,
		ResolvedProfile:     resolved.ResolvedProfile,
	}, nil
}

func (c *Catalog) ListSessions(ctx context.Context, params protocol.SessionListParams) (protocol.SessionListResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.validateHostEpochLocked(params.ExpectedHostEpoch); err != nil {
		return protocol.SessionListResult{}, err
	}
	limit, err := normalizedLimit(params.Limit)
	if err != nil {
		return protocol.SessionListResult{}, err
	}
	workspace, ok := c.state.Workspaces[params.WorkspaceID]
	if !ok || !workspace.Open {
		return protocol.SessionListResult{}, catalogError(protocol.ErrWorkspaceNotFound, errors.New("workspace is not open"))
	}
	if err := c.syncWorkspaceSessionsLocked(ctx, workspace); err != nil {
		return protocol.SessionListResult{}, err
	}
	records := c.liveSessionRecordsLocked(params.WorkspaceID)
	start, err := c.pageStartLocked("session/list", string(params.WorkspaceID), params.Cursor, c.state.Revision)
	if err != nil {
		return protocol.SessionListResult{}, err
	}
	end := start + limit
	if end > len(records) {
		end = len(records)
	}
	items := make([]protocol.SessionSummary, 0, end-start)
	for _, record := range records[start:end] {
		meta, err := c.validateLiveSessionRecordLocked(workspace, record)
		if err != nil {
			return protocol.SessionListResult{}, err
		}
		topic := c.state.Topics[params.WorkspaceID][record.TopicID]
		target := protocol.RuntimeTarget{WorkspaceID: params.WorkspaceID, SessionID: record.ID}
		created, activity := metadataTimes(meta, c.now().UTC())
		item := protocol.SessionSummary{
			Target:              target,
			TopicID:             record.TopicID,
			Title:               sessionTitle(meta, topic.Title),
			Preview:             meta.Preview,
			Turns:               maxInt(meta.Turns, 0),
			CreatedAtMs:         nonnegativeUnixMilli(created),
			LastActivityAtMs:    nonnegativeUnixMilli(activity),
			RecoveryInterrupted: meta.Recovered || meta.InFlightTurn != nil,
		}
		if meta.RemoteParentSessionID != "" && meta.RemoteParentCheckpointID != "" {
			item.BranchSource = &protocol.BranchSource{
				ParentTarget:       protocol.RuntimeTarget{WorkspaceID: params.WorkspaceID, SessionID: protocol.SessionID(meta.RemoteParentSessionID)},
				ParentCheckpointID: protocol.CheckpointID(meta.RemoteParentCheckpointID),
			}
		}
		if c.runtimeInspector != nil {
			if runtime, exists := c.runtimeInspector.SessionSummary(target); exists {
				copyValue := *runtime
				item.Runtime = &copyValue
			}
		}
		items = append(items, item)
	}
	hasMore := end < len(records)
	var next protocol.Cursor
	if hasMore {
		next, err = c.storeCursorLocked(cursorRecord{Method: "session/list", Binding: string(params.WorkspaceID), Revision: c.state.Revision, Offset: end})
		if err != nil {
			return protocol.SessionListResult{}, err
		}
	}
	return protocol.SessionListResult{Items: items, HasMore: hasMore, NextCursor: next}, nil
}

func (c *Catalog) liveSessionRecordsLocked(workspaceID protocol.WorkspaceID) []sessionRecord {
	records := make([]sessionRecord, 0)
	for _, record := range c.state.Sessions {
		if record.WorkspaceID == workspaceID && record.Path != "" && record.TrashPath == "" {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		metaI, _, _ := agent.LoadBranchMeta(records[i].Path)
		metaJ, _, _ := agent.LoadBranchMeta(records[j].Path)
		_, activityI := metadataTimes(metaI, time.Time{})
		_, activityJ := metadataTimes(metaJ, time.Time{})
		if !activityI.Equal(activityJ) {
			return activityI.After(activityJ)
		}
		return records[i].ID < records[j].ID
	})
	return records
}

func metadataTimes(meta agent.BranchMeta, fallback time.Time) (time.Time, time.Time) {
	created := meta.CreatedAt
	if created.IsZero() {
		created = fallback
	}
	activity := meta.UpdatedAt
	if activity.IsZero() {
		activity = created
	}
	return created.UTC(), activity.UTC()
}

func sessionTitle(meta agent.BranchMeta, topicTitle string) string {
	if title := strings.TrimSpace(meta.CustomTitle); title != "" {
		return title
	}
	if preview := strings.TrimSpace(meta.Preview); preview != "" {
		return preview
	}
	if topicTitle = strings.TrimSpace(topicTitle); topicTitle != "" {
		return topicTitle
	}
	return defaultTopicTitle
}

func profileFromMeta(meta agent.BranchMeta) protocol.ResolvedProfile {
	return protocol.ResolvedProfile{
		Model:             meta.Model,
		Effort:            meta.Effort,
		CollaborationMode: protocol.CollaborationMode(meta.Mode),
		TokenMode:         protocol.TokenMode(meta.TokenMode),
		ToolApprovalMode:  protocol.ToolApprovalMode(meta.ToolApprovalMode),
	}
}

func profileSelectionFromMeta(meta agent.BranchMeta) protocol.ProfileSelection {
	var selection protocol.ProfileSelection
	if value := strings.TrimSpace(meta.Model); value != "" {
		selection.Model = &value
	}
	if value := strings.TrimSpace(meta.Effort); value != "" {
		selection.Effort = &value
	}
	if value := strings.TrimSpace(meta.Mode); value != "" {
		mode := protocol.CollaborationMode(value)
		selection.CollaborationMode = &mode
	}
	if value := strings.TrimSpace(meta.TokenMode); value != "" {
		mode := protocol.TokenMode(value)
		selection.TokenMode = &mode
	}
	if value := strings.TrimSpace(meta.ToolApprovalMode); value != "" {
		mode := protocol.ToolApprovalMode(value)
		selection.ToolApprovalMode = &mode
	} else {
		// Legacy Desktop encoded YOLO in the combined Mode axis, but an empty
		// approval on every other legacy Session meant interactive ask. New
		// Session creation intentionally leaves the selection empty so the
		// current Host user default applies; migration must freeze the safer old
		// meaning instead of silently upgrading old conversations to auto/yolo.
		switch strings.ToLower(strings.TrimSpace(meta.Mode)) {
		case "yolo", "plan-yolo", "yolo-plan":
			// The Host resolver decodes the legacy combined axis and preserves
			// YOLO. Supplying ask here would erase explicit legacy intent.
		default:
			mode := protocol.ToolApprovalAsk
			selection.ToolApprovalMode = &mode
		}
	}
	return selection
}

func (c *Catalog) validateLiveSessionRecordLocked(workspace workspaceRecord, record sessionRecord) (agent.BranchMeta, error) {
	sessionDir, err := c.sessionStoreDirectoryLocked(workspace, false)
	if err != nil || !sessionPathInStore(sessionDir, record.Path) {
		return agent.BranchMeta{}, catalogError(protocol.ErrSessionPersistFailed, errors.New("Session artifact escaped its workspace store"))
	}
	info, err := os.Lstat(record.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return agent.BranchMeta{}, catalogError(protocol.ErrSessionNotFound, err)
		}
		return agent.BranchMeta{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return agent.BranchMeta{}, catalogError(protocol.ErrSessionPersistFailed, errors.New("Session transcript is not a regular file"))
	}
	if err := validateMetaSidecar(record.Path, true); err != nil {
		return agent.BranchMeta{}, err
	}
	meta, ok, err := agent.LoadBranchMeta(record.Path)
	if err != nil || !ok {
		if err == nil {
			err = errors.New("Session sidecar is missing")
		}
		return agent.BranchMeta{}, catalogError(protocol.ErrSessionPersistFailed, err)
	}
	if protocol.SessionID(meta.RemoteSessionID) != record.ID {
		return agent.BranchMeta{}, catalogError(protocol.ErrSessionPersistFailed, errors.New("Session sidecar identity does not match registry"))
	}
	if canonical, err := canonicalExistingDirectory(meta.WorkspaceRoot); err != nil || pathKey(canonical) != pathKey(workspace.CanonicalPath) {
		return agent.BranchMeta{}, catalogError(protocol.ErrWorkspaceSessionMismatch, errors.New("Session sidecar belongs to another workspace"))
	}
	return meta, nil
}

func pathWithin(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func removeSessionArtifacts(sessionPath string) error {
	paths := []string{sessionPath}
	paths = append(paths, store.SessionSidecarFiles(sessionPath)...)
	paths = append(paths, store.SessionCheckpointDir(sessionPath), store.SessionJobsDir(sessionPath), store.SessionCleanupPending(sessionPath))
	var combined error
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}
