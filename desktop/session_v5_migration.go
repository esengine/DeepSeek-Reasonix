package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/fileutil"
	"reasonix/internal/session"
	"reasonix/internal/store"
)

type desktopMigrationRecord struct {
	SourceKey           string                       `json:"sourceKey"`
	TargetSessionID     string                       `json:"targetSessionId"`
	ContentDigest       string                       `json:"contentDigest,omitempty"`
	SourceRevision      string                       `json:"sourceRevision,omitempty"`
	Status              string                       `json:"status"`
	ErrorCode           string                       `json:"errorCode,omitempty"`
	Attempts            int                          `json:"attempts"`
	PreviousCompletion  *desktopMigrationReceipt     `json:"previousCompletion,omitempty"`
	LegacyHeads         []string                     `json:"legacyHeads,omitempty"`
	LegacySelectedHead  string                       `json:"legacySelectedHead,omitempty"`
	LegacyPrimaryHead   string                       `json:"legacyPrimaryHead,omitempty"`
	LegacyHeadsRevision string                       `json:"legacyHeadsRevision,omitempty"`
	LegacyAdoption      *desktopMigrationReceipt     `json:"legacyAdoption,omitempty"`
	LegacyConversions   []desktopMigrationConversion `json:"legacyConversions,omitempty"`
}

type desktopMigrationLedger struct {
	Version int                               `json:"version"`
	Records map[string]desktopMigrationRecord `json:"records"`
}

var desktopMigrationMu sync.Mutex

func desktopMigrationLedgerPath() string {
	return filepath.Join(desktopConfigDir(), "desktop", "session-migration-v5.json")
}

func (a *App) startDesktopSessionMigration(ctx context.Context) {
	if a == nil {
		return
	}
	// Capture reservations before startup admits renderer requests. Background
	// replay must not abort a new create whose body has not been published yet.
	startupState, err := a.workspaceRegistry().Load(ctx)
	if err != nil {
		a.desktopMigrationFailed.Store(true)
		slogWarnDesktopMigration(err)
		close(a.desktopMigrationDone)
		return
	}
	go func() {
		defer close(a.desktopMigrationDone)
		if err := a.backupDesktopUpgradeMetadata(ctx); err != nil {
			a.desktopMigrationFailed.Store(true)
			slogWarnDesktopMigration(err)
			return
		}
		if err := a.recoverDesktopPendingCreateSnapshot(ctx, startupState.PendingCreates); err != nil {
			a.desktopMigrationFailed.Store(true)
			slogWarnDesktopMigration(err)
		}
		if err := a.migrateDesktopSessionsV5(ctx); err != nil {
			a.desktopMigrationFailed.Store(true)
			slogWarnDesktopMigration(err)
			// A handled migration failure no longer terminates the process, so
			// run a second drain after its diagnostic has entered the queue.
			a.goSafe("flushPendingCrash", a.flushPendingCrash)
		}
		for _, run := range []func(context.Context) error{a.recoverDesktopSessionOperations, a.discoverHistoricalTrash, a.reconcileUnregisteredSessions} {
			if err := run(ctx); err != nil {
				a.desktopMigrationFailed.Store(true)
				slogWarnDesktopMigration(err)
			}
		}
		a.emitProjectTreeChanged()
	}()
}

// recoverDesktopPendingCreates completes the registry half of a create that
// reached durable session publication before the process stopped. A missing
// target is safe to forget: no canonical content exists for the pending ID and
// the UI can retry creation without inventing a replacement identity.
func (a *App) recoverDesktopPendingCreates(ctx context.Context) error {
	state, err := a.workspaceRegistry().Load(ctx)
	if err != nil {
		return err
	}
	return a.recoverDesktopPendingCreateSnapshot(ctx, state.PendingCreates)
}

func (a *App) recoverDesktopPendingCreateSnapshot(ctx context.Context, pendingCreates map[string]workspacestate.PendingCreate) error {
	service := a.desktopSessionService("")
	var joined error
	for sessionID, pending := range pendingCreates {
		ref := session.SessionRef{HostID: localDesktopHostID, SessionID: sessionID}
		if _, err := service.Query().Snapshot(ctx, ref); err == nil {
			var attachErr error
			if strings.HasPrefix(pending.OperationID, "rotate-") {
				attachErr = a.workspaceRegistry().CommitRotation(ctx, pending.OperationID, pending.WorkspaceID, sessionID, "", pending.ArchiveSource)
			} else {
				attachErr = a.workspaceRegistry().AttachSession(ctx, pending.OperationID, pending.WorkspaceID, sessionID, "")
			}
			if attachErr != nil {
				joined = errors.Join(joined, attachErr)
			} else {
				a.desktopSessions.pendingCreateRecovered.Add(1)
			}
		} else if errors.Is(err, session.ErrSessionNotFound) {
			if abortErr := a.workspaceRegistry().AbortCreate(ctx, sessionID); abortErr != nil {
				joined = errors.Join(joined, abortErr)
			}
		} else {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func slogWarnDesktopMigration(err error) {
	// Keep migration logs content- and path-free. Detailed per-source state is
	// available through the local ledger and UI health row.
	if err != nil {
		slog.Warn("desktop session migration incomplete")
	}
}

type desktopMigrationSource struct {
	operationID         string
	headID              string
	deferArchive        bool
	versionFingerprint  string
	root                string
	scope               string
	workspaceRoot       string
	exact               map[string]bool
	records             map[string]desktopMigrationRecord
	pairedRoot          string
	pairedIDs           map[string]bool
	legacyAdoption      *desktopMigrationReceipt
	pairedAdoption      *desktopMigrationReceipt
	conversions         map[string][]desktopMigrationConversion
	headConversions     []desktopMigrationConversion
	handledStores       map[string]bool
	conversionAdoptions []*desktopMigrationReceipt
}

func (a *App) migrateDesktopSessionsV5(ctx context.Context) error {
	replayErr := a.recoverDesktopSessionOperations(ctx)
	tabs := loadTabsFile()
	projects := loadProjectsFile()
	sources := map[string]*desktopMigrationSource{}
	add := func(scope, workspaceRoot, root string) *desktopMigrationSource {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "." || root == "" || sameDesktopPath(root, a.desktopSessions.root) {
			return nil
		}
		key := canonicalRuntimeRoot(root)
		if current := sources[key]; current != nil {
			return current
		}
		source := &desktopMigrationSource{root: root, scope: scope, workspaceRoot: workspaceRoot, exact: map[string]bool{}}
		sources[key] = source
		return source
	}
	addStores := func(scope, workspaceRoot, root string) {
		if strings.TrimSpace(root) == "" {
			return
		}
		for _, candidate := range desktopLegacyStoreRoots(root) {
			add(scope, workspaceRoot, candidate)
		}
	}
	addStores("global", "", config.SessionStoreDir())
	addStores("global", "", config.ProjectSessionStoreDir(globalWorkspaceRoot()))
	for _, project := range projects.Projects {
		addStores("project", project.Root, config.ProjectSessionStoreDir(project.Root))
	}
	for _, tab := range tabs.Tabs {
		if strings.TrimSpace(tab.SessionID) == "" {
			continue
		}
		root := config.ProjectSessionStoreDir(globalWorkspaceRoot())
		if tab.Scope == "project" {
			root = config.ProjectSessionStoreDir(tab.WorkspaceRoot)
		}
		if source := add(tab.Scope, tab.WorkspaceRoot, root); source != nil {
			source.exact[tab.SessionID] = true
		}
		addStores(tab.Scope, tab.WorkspaceRoot, root)
	}
	joined := replayErr
	legacySources := map[string]desktopMigrationSource{}
	addLegacy := func(scope, workspaceRoot, dir string) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "." || dir == "" {
			return
		}
		key := canonicalRuntimeRoot(dir)
		if _, ok := legacySources[key]; !ok {
			legacySources[key] = desktopMigrationSource{root: dir, scope: scope, workspaceRoot: workspaceRoot, exact: map[string]bool{}, pairedRoot: filepath.Join(filepath.Dir(dir), "sessions-v4")}
		}
	}
	addLegacy("global", "", config.SessionDir())
	addLegacy("global", "", desktopSessionDir(globalWorkspaceRoot()))
	for _, project := range projects.Projects {
		addLegacy("project", project.Root, desktopSessionDir(project.Root))
	}
	for _, tab := range tabs.Tabs {
		path := filepath.Clean(strings.TrimSpace(tab.SessionPath))
		if path == "." || path == "" {
			continue
		}
		dir := filepath.Dir(path)
		key := canonicalRuntimeRoot(dir)
		source, ok := legacySources[key]
		if !ok {
			source = desktopMigrationSource{root: dir, scope: tab.Scope, workspaceRoot: tab.WorkspaceRoot, exact: map[string]bool{}, pairedRoot: filepath.Join(filepath.Dir(dir), "sessions-v4")}
		}
		source.exact[path] = true
		legacySources[key] = source
	}
	// Include retired roots discovered only through saved legacy tabs before
	// indexing conversion provenance.
	for _, source := range legacySources {
		addStores(source.scope, source.workspaceRoot, source.pairedRoot)
	}
	conversions, handledStores, conversionErr := discoverDesktopMigrationConversions(ctx, sources)
	joined = errors.Join(joined, conversionErr)
	for _, source := range legacySources {
		source.conversions, source.handledStores = conversions, handledStores
		joined = errors.Join(joined, markDesktopMigrationPairedStores(source, sources))
		if err := a.migrateLegacyDirectory(ctx, source); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	joined = errors.Join(joined, a.migrateStoredConversionLineages(ctx, conversions, sources, handledStores))
	for _, source := range sources {
		source.handledStores = handledStores
		if err := a.migrateCanonicalStore(ctx, *source); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return errors.Join(joined, a.discoverHistoricalTrash(ctx), a.reconcileUnregisteredSessions(ctx))
}

// A paired checkpoint and event store are one migration decision. Never
// publish the sidecar independently after a conflict or source failure.
func markDesktopMigrationPairedStores(source desktopMigrationSource, sources map[string]*desktopMigrationSource) error {
	entries, err := os.ReadDir(source.root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var joined error
	for _, entry := range entries {
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(source.root, entry.Name())
		pairedRoot, err := desktopLegacyPairedRoot(path, source.pairedRoot)
		if err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		if paired := sources[canonicalRuntimeRoot(pairedRoot)]; paired != nil {
			if paired.pairedIDs == nil {
				paired.pairedIDs = map[string]bool{}
			}
			paired.pairedIDs[agent.BranchID(path)] = true
		}
	}
	return joined
}

func (a *App) migrateCanonicalStore(ctx context.Context, source desktopMigrationSource) (retErr error) {
	if _, err := os.Stat(source.root); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	persistence := session.NewFilesystemPersistence(source.root)
	old, err := session.NewService("migration-source", persistence)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, old.Shutdown(context.Background())) }()
	ledger, err := readDesktopMigrationLedger()
	if err != nil {
		return err
	}
	source.records = ledger.Records
	var joined error
	var cursor string
	for {
		// A disposable catalog cache cannot decide whether durable history
		// exists. Enumerate every identity, including entries with failed or
		// missing metadata, without scheduling writes to the source cache.
		page, err := persistence.List(ctx, cursor, 100)
		if err != nil {
			return errors.Join(joined, err)
		}
		for _, info := range page.Sessions {
			if ctx.Err() != nil {
				return errors.Join(joined, ctx.Err())
			}
			if source.pairedIDs[info.SessionID] || source.handledStores[canonicalRuntimeRoot(filepath.Join(source.root, info.SessionID))] {
				continue
			}
			if info.Codec == session.PrototypeCodec || info.Codec == session.LegacyLinearCodec || info.Codec == session.FinalV31Codec {
				joined = errors.Join(joined, a.migratePreviewSession(ctx, source, info.SessionID))
				continue
			}
			if err := a.migrateCanonicalSession(ctx, old, source, "", info.SessionID); err != nil {
				joined = errors.Join(joined, err)
			}
		}
		if page.NextCursor == "" {
			return joined
		}
		if page.NextCursor <= cursor {
			return errors.Join(joined, errors.New("desktop migration source cursor did not advance"))
		}
		cursor = page.NextCursor
	}
}

func (a *App) migrateCanonicalSession(ctx context.Context, old *session.Service, source desktopMigrationSource, workspaceID, sessionID string) error {
	preview, err := isDesktopStoredPreview(filepath.Join(source.root, sessionID))
	if err != nil {
		return errors.Join(err, updateDesktopMigrationLedger(desktopCanonicalMigrationKey(source.root, sessionID), sessionID, "failed", "source_read"))
	}
	if preview {
		return a.migratePreviewSession(ctx, source, sessionID)
	}
	key := desktopCanonicalMigrationKey(source.root, sessionID)
	if source.versionFingerprint != "" {
		key += ":review:" + source.versionFingerprint
	}
	checkpoint, err := newDesktopMigrationCheckpoint(source, key, canonicalMigrationSourceFiles(source.root, sessionID))
	if err != nil {
		return err
	}
	if handled, err := a.checkAdoptedMigrationSource(ctx, source, checkpoint); handled || err != nil {
		return err
	}
	if checkpoint.unchanged() {
		return a.completeRegisteredMigration(ctx, source, checkpoint, checkpoint.record.TargetSessionID, checkpoint.record.ContentDigest)
	}
	oldRef := session.SessionRef{HostID: "migration-source", SessionID: sessionID}
	contentDigest, err := canonicalMigrationDigest(ctx, old.Query(), oldRef)
	if err != nil {
		return errors.Join(err, updateDesktopMigrationLedger(key, sessionID, "failed", "source_read"))
	}
	if handled, err := a.quarantineChangedMigration(ctx, source, checkpoint, contentDigest); handled || err != nil {
		return err
	}
	if checkpoint.matchesCompletedContent(contentDigest) {
		return a.completeRegisteredMigration(ctx, source, checkpoint, checkpoint.record.TargetSessionID, contentDigest)
	}
	if workspaceID == "" {
		workspaceID, err = a.ensureDesktopMigrationWorkspace(ctx, source)
		if err != nil {
			return err
		}
	}
	target := a.desktopSessionService("")
	targetID, needsImport, err := a.resolveRegisteredMigrationTarget(ctx, source, checkpoint, sessionID, contentDigest)
	if err != nil {
		_ = updateDesktopMigrationLedger(key, sessionID, "failed", "target_conflict", contentDigest)
		return err
	}
	if err := updateDesktopMigrationLedger(key, targetID, "pending", "", contentDigest); err != nil {
		return err
	}
	if err := a.prepareRegisteredMigration(ctx, source, checkpoint, targetID, workspaceID); err != nil {
		return err
	}
	if needsImport {
		tmp, err := os.MkdirTemp("", "reasonix-session-v5-export-")
		if err != nil {
			return err
		}
		bundle := filepath.Join(tmp, "bundle")
		defer os.RemoveAll(tmp)
		if err := old.Export(ctx, oldRef, bundle); err != nil {
			_ = updateDesktopMigrationLedger(key, targetID, "failed", "export", contentDigest)
			return err
		}
		if _, err := target.ImportWithHeader(ctx, bundle, session.CreateOptions{
			SessionID: targetID, CWD: desktopWorkspaceRoot(source.scope, source.workspaceRoot), Origin: session.SessionOriginCanonicalImport,
		}); err != nil {
			_ = updateDesktopMigrationLedger(key, targetID, "failed", "import", contentDigest)
			return err
		}
	}

	return a.completeRegisteredMigration(ctx, source, checkpoint, targetID, contentDigest)
}

func (a *App) migrateLegacyDirectory(ctx context.Context, source desktopMigrationSource) error {
	entries, err := os.ReadDir(source.root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	ledger, err := readDesktopMigrationLedger()
	if err != nil {
		return err
	}
	source.records = ledger.Records
	var joined error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(source.root, entry.Name())
		if desktopMigrationAutomaticRecovery(path) {
			continue
		}
		perFile := source
		perFile.pairedRoot, err = desktopLegacyPairedRoot(path, source.pairedRoot)
		if err != nil {
			joined = errors.Join(joined, err, updateDesktopMigrationLedger(desktopLegacyMigrationKey(path), "", "failed", "source_stat"))
			continue
		}
		if err := a.migrateLegacyHeads(ctx, path, perFile); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (a *App) migrateLegacySession(ctx context.Context, path string, source desktopMigrationSource, workspaceID string) (retErr error) {
	key := desktopLegacyMigrationKey(path)
	if source.headID != "" {
		key = desktopLegacyHeadKey(path, source.headID)
	}
	return a.migrateLegacyHead(ctx, path, source, workspaceID, source.headID, key, true)
}

func (a *App) migrateLegacyHead(ctx context.Context, path string, source desktopMigrationSource, workspaceID, headID, key string, paired bool) (retErr error) {
	source.headID = headID
	if source.operationID == "" {
		meta, exists, err := agent.LoadBranchMeta(path)
		if err != nil {
			return errors.Join(err, a.sourceRecovery(ctx, path, "legacy", "metadata_unreadable", source.scope, source.workspaceRoot, headID))
		}
		if exists && meta.WorkspaceRoot != "" && !sameDesktopPath(meta.WorkspaceRoot, desktopWorkspaceRoot(source.scope, source.workspaceRoot)) {
			return errors.Join(errSessionWorkspaceConflict, a.sourceRecovery(ctx, path, "legacy", "workspace_conflict", source.scope, source.workspaceRoot, headID))
		}
	}
	if source.versionFingerprint != "" {
		key += ":review:" + source.versionFingerprint
	}
	checkpoint, err := newDesktopMigrationCheckpoint(source, key, desktopLegacyMigrationFiles(path, source))
	if err != nil {
		return err
	}
	if handled, err := a.checkAdoptedMigrationSource(ctx, source, checkpoint); handled || err != nil {
		return err
	}
	if checkpoint.unchanged() {
		return a.completeRegisteredMigration(ctx, source, checkpoint, checkpoint.record.TargetSessionID, checkpoint.record.ContentDigest)
	}
	if len(source.headConversions) > 0 {
		if err := a.migrateConversionLineage(ctx, path, headID, source, &checkpoint, workspaceID); err != nil {
			return errors.Join(err, updateDesktopMigrationLedger(key, checkpoint.record.TargetSessionID, "failed", "conversion_import"))
		}
		return nil
	}
	if paired && source.pairedRoot != "" {
		// Previous v5 builds may already have adopted the paired canonical
		// store before discovering its metadata-less legacy checkpoint.
		pairedKey := desktopCanonicalMigrationKey(source.pairedRoot, agent.BranchID(path))
		record := source.records[pairedKey]
		if record.Status == "completed" {
			source.pairedAdoption = &desktopMigrationReceipt{TargetSessionID: record.TargetSessionID, ContentDigest: record.ContentDigest}
		} else {
			source.pairedAdoption = record.PreviousCompletion
		}
	}
	stageRoot, err := os.MkdirTemp("", "reasonix-legacy-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageRoot)
	stage, err := session.NewService("migration-stage", session.NewFilesystemPersistence(filepath.Join(stageRoot, "sessions-v4")))
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, stage.Shutdown(context.Background())) }()
	var runtime *session.Runtime
	if paired && source.pairedRoot != "" {
		runtime, _, err = stage.ContinueImportedFrom(ctx, path, source.pairedRoot, headID)
	} else {
		runtime, _, err = stage.ContinueImported(ctx, path, headID)
	}
	if err != nil {
		_ = updateDesktopMigrationLedger(key, "", "failed", "legacy_import")
		var diagnostic *session.TranscriptInitializationError
		if errors.As(err, &diagnostic) {
			// The source key matches the local migration ledger. Never log the
			// source path or the unrestricted error string from imported data.
			slog.Warn("desktop session migration transcript initialization failed", "source_key", key,
				"stage", "legacy_import", "diagnostic", diagnostic)
			queueTranscriptInitializationFailure(diagnostic)
		}
		return err
	}
	if err := stage.Close(ctx, runtime.Ref()); err != nil {
		return err
	}
	return a.publishStagedMigration(ctx, source, checkpoint, stage, runtime.Ref(), workspaceID, session.SessionOriginLegacyImport)
}

func (a *App) publishStagedMigration(ctx context.Context, source desktopMigrationSource, checkpoint desktopMigrationCheckpoint, stage *session.Service, ref session.SessionRef, workspaceID string, origin session.SessionOrigin) error {
	key := checkpoint.key
	target := a.desktopSessionService("")
	contentDigest, err := canonicalMigrationDigest(ctx, stage.Query(), ref)
	if err != nil {
		return err
	}
	if handled, err := a.quarantineChangedMigration(ctx, source, checkpoint, contentDigest); handled || err != nil {
		return err
	}
	if checkpoint.matchesCompletedContent(contentDigest) {
		return a.completeRegisteredMigration(ctx, source, checkpoint, checkpoint.record.TargetSessionID, contentDigest)
	}
	// An older migrator adopted only the selected DAG head under the path key.
	// Recognize that receipt when adding explicit head identities, even if the
	// selected head has since changed and the v5 target has been continued.
	if source.legacyAdoption != nil && source.legacyAdoption.ContentDigest == contentDigest {
		return a.completeRegisteredMigration(ctx, source, checkpoint, source.legacyAdoption.TargetSessionID, contentDigest)
	}
	if source.pairedAdoption != nil && source.pairedAdoption.ContentDigest == contentDigest {
		return a.completeRegisteredMigration(ctx, source, checkpoint, source.pairedAdoption.TargetSessionID, contentDigest)
	}
	for _, receipt := range source.conversionAdoptions {
		if receipt.ContentDigest == contentDigest {
			return a.completeRegisteredMigration(ctx, source, checkpoint, receipt.TargetSessionID, contentDigest)
		}
	}
	if workspaceID == "" {
		workspaceID, err = a.ensureDesktopMigrationWorkspace(ctx, source)
		if err != nil {
			return err
		}
	}
	targetID, needsImport, err := a.resolveRegisteredMigrationTarget(ctx, source, checkpoint, ref.SessionID, contentDigest)
	if err != nil {
		_ = updateDesktopMigrationLedger(key, ref.SessionID, "failed", "target_conflict", contentDigest)
		return err
	}
	if err := updateDesktopMigrationLedger(key, targetID, "pending", "", contentDigest); err != nil {
		return err
	}
	if err := a.prepareRegisteredMigration(ctx, source, checkpoint, targetID, workspaceID); err != nil {
		return err
	}
	if needsImport {
		tmp, err := os.MkdirTemp("", "reasonix-v5-bundle-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		bundle := filepath.Join(tmp, "bundle")
		if err := stage.Export(ctx, ref, bundle); err != nil {
			return errors.Join(err, updateDesktopMigrationLedger(key, targetID, "failed", "export", contentDigest))
		}
		if _, err := target.ImportWithHeader(ctx, bundle, session.CreateOptions{
			SessionID: targetID, CWD: desktopWorkspaceRoot(source.scope, source.workspaceRoot), Origin: origin,
		}); err != nil {
			_ = updateDesktopMigrationLedger(key, targetID, "failed", "import", contentDigest)
			return err
		}
	}

	return a.completeRegisteredMigration(ctx, source, checkpoint, targetID, contentDigest)
}

func canonicalMigrationDigest(ctx context.Context, query *session.Query, ref session.SessionRef) (string, error) {
	messages, err := query.History(ctx, ref)
	if err != nil {
		return "", err
	}
	return agent.ContentDigestForMessages(messages)
}

func resolveMigrationTarget(ctx context.Context, query *session.Query, preferredID, sourceKey, contentDigest string, sourcePaths ...string) (string, bool, error) {
	check := func(sessionID string) (bool, error) {
		digest, err := canonicalMigrationDigest(ctx, query, session.SessionRef{HostID: localDesktopHostID, SessionID: sessionID})
		if errors.Is(err, session.ErrSessionNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if digest != contentDigest {
			return false, nil
		}
		if len(sourcePaths) > 0 {
			info, err := query.Stat(ctx, session.SessionRef{HostID: localDesktopHostID, SessionID: sessionID})
			if err != nil {
				return false, err
			}
			body, err := os.ReadFile(filepath.Join(info.Path, "manifest.json"))
			if err != nil {
				return false, err
			}
			var manifest session.Manifest
			if err := json.Unmarshal(body, &manifest); err != nil {
				return false, err
			}
			if manifest.Source == nil || !sameDesktopPath(manifest.Source.Path, sourcePaths[0]) {
				return false, nil
			}
			if len(sourcePaths) > 1 && sourcePaths[1] != "" && manifest.Source.LegacyHeadID != sourcePaths[1] {
				return false, nil
			}
		}
		return true, nil
	}
	if identical, err := check(preferredID); err != nil {
		return "", false, err
	} else if identical {
		return preferredID, false, nil
	} else if _, err := query.Snapshot(ctx, session.SessionRef{HostID: localDesktopHostID, SessionID: preferredID}); errors.Is(err, session.ErrSessionNotFound) {
		return preferredID, true, nil
	} else if err != nil {
		return "", false, err
	}
	digest := sha256.Sum256([]byte(sourceKey + "\x00" + contentDigest))
	conflictID := "migr-" + hex.EncodeToString(digest[:12])
	if identical, err := check(conflictID); err != nil {
		return "", false, err
	} else if identical {
		return conflictID, false, nil
	} else if _, err := query.Snapshot(ctx, session.SessionRef{HostID: localDesktopHostID, SessionID: conflictID}); errors.Is(err, session.ErrSessionNotFound) {
		return conflictID, true, nil
	} else if err != nil {
		return "", false, err
	}
	return "", false, errors.New("migration target identity collision")
}

// sourceState optionally supplies the content digest followed by a file revision.
func updateDesktopMigrationLedger(sourceKey, targetID, status, errorCode string, sourceState ...string) error {
	desktopMigrationMu.Lock()
	defer desktopMigrationMu.Unlock()
	path := desktopMigrationLedgerPath()
	release, lockErr := lockDesktopMigrationLedger()
	if lockErr != nil {
		return lockErr
	}
	defer release()
	ledger, original, err := readDesktopMigrationLedgerFile()
	if err != nil {
		return err
	}
	record := ledger.Records[sourceKey]
	// A failed scan or interrupted update must not erase proof that an older
	// source revision was already adopted (and may have been continued).
	if record.Status == "completed" && status != "completed" {
		record.PreviousCompletion = &desktopMigrationReceipt{
			TargetSessionID: record.TargetSessionID, ContentDigest: record.ContentDigest, SourceRevision: record.SourceRevision,
		}
	}
	record.SourceKey, record.TargetSessionID, record.Status, record.ErrorCode = sourceKey, targetID, status, errorCode
	if len(sourceState) > 0 {
		record.ContentDigest = sourceState[0]
	}
	record.SourceRevision = ""
	if status == "completed" && len(sourceState) > 1 {
		record.SourceRevision = sourceState[1]
	}
	if status == "completed" {
		record.PreviousCompletion = nil
	}
	if status == "pending" {
		record.Attempts++
	}
	ledger.Records[sourceKey] = record
	body, err := marshalDesktopMigrationRecord(original, ledger, sourceKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, append(body, '\n'), 0o600)
}
