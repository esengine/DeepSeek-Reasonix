package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/topicstate"
)

const (
	topicStateLockTimeout      = 2 * time.Second
	topicStateOperationTimeout = 5 * time.Second
)

type topicStateManager struct {
	mu               sync.Mutex
	scopes           map[string]*topicStateScope
	now              func() time.Time
	operationContext func() (context.Context, context.CancelFunc)
}

type topicStateScope struct {
	mu    sync.Mutex
	root  string
	path  string
	store *topicstate.Store
}

type legacyTopicSnapshot struct {
	titles     map[string]string
	sources    map[string]string
	createdAts map[string]int64
	autoMeta   map[string]json.RawMessage
	digests    [4]string
	exists     bool
}

var desktopTopicState = newTopicStateManager()

// topicLegacyWriteHookForTest injects deterministic mirror failures after the
// authoritative SQLite transaction has committed. Production leaves it nil.
var topicLegacyWriteHookForTest func(string) error

func newTopicStateManager() *topicStateManager {
	return &topicStateManager{
		scopes: map[string]*topicStateScope{}, now: time.Now,
		operationContext: newTopicOperationContext,
	}
}

func (m *topicStateManager) scope(workspaceRoot string) *topicStateScope {
	workspaceRoot = normalizeProjectRoot(workspaceRoot)
	path := config.DesktopTopicStatePath(workspaceRoot)
	key := path
	if key == "" {
		key = "missing:" + workspaceRoot
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if scope := m.scopes[key]; scope != nil {
		return scope
	}
	scope := &topicStateScope{root: workspaceRoot, path: path}
	m.scopes[key] = scope
	return scope
}

func (m *topicStateManager) close() {
	m.mu.Lock()
	scopes := make([]*topicStateScope, 0, len(m.scopes))
	for _, scope := range m.scopes {
		scopes = append(scopes, scope)
	}
	m.scopes = map[string]*topicStateScope{}
	m.mu.Unlock()
	for _, scope := range scopes {
		scope.mu.Lock()
		if scope.store != nil {
			_ = scope.store.Close()
			scope.store = nil
		}
		scope.mu.Unlock()
	}
}

func (m *topicStateManager) snapshot(workspaceRoot string) (topicstate.Snapshot, error) {
	scope := m.scope(workspaceRoot)
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if err := m.ensureOpenLocked(scope); err != nil {
		return topicstate.Snapshot{}, err
	}
	return scope.store.Snapshot(context.Background())
}

func (m *topicStateManager) mutate(workspaceRoot string, mutation func(context.Context, *topicstate.Store) (topicstate.State, error), legacyFallback func() error) error {
	scope := m.scope(workspaceRoot)
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.root != "" && !existingDirectory(scope.root) {
		return fmt.Errorf("workspace root %q no longer exists", scope.root)
	}
	if scope.path == "" {
		return errors.New("topic state directory is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(scope.path), 0o700); err != nil {
		return err
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), topicStateLockTimeout)
	release, err := filelock.Acquire(lockCtx, scope.path+".compat.lock")
	cancelLock()
	if err != nil {
		return err
	}
	defer release()
	if err := m.ensureOpenAndReconcileLocked(scope); err != nil {
		if legacyFallback != nil && legacyTopicFilesExist(scope.root) && legacyFallbackAllowed(err) {
			slog.Warn("desktop: topic state using legacy write fallback", "scope", topicScopeKind(scope.root), "error_type", topicStateErrorType(err))
			return legacyFallback()
		}
		return err
	}
	// Opening/reconciling uses its own context. Start the mutation's budget only
	// after that phase; cold SQLite startup must not hand it an expired context.
	ctx, cancelOperation := m.operationContext()
	defer cancelOperation()
	if _, err := mutation(ctx, scope.store); err != nil {
		return err
	}
	if err := m.mirrorIfPendingLocked(ctx, scope); err != nil {
		// The SQLite mutation is already authoritative. Keep the pending outbox
		// for startup/next-write repair instead of reporting a false rollback.
		slog.Warn("desktop: topic legacy mirror pending", "scope", topicScopeKind(scope.root), "error_type", topicStateErrorType(err))
	}
	return nil
}

func (m *topicStateManager) ensureOpenLocked(scope *topicStateScope) error {
	if scope.store != nil {
		return nil
	}
	if scope.path == "" {
		return errors.New("topic state directory is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(scope.path), 0o700); err != nil {
		return err
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), topicStateLockTimeout)
	release, err := filelock.Acquire(lockCtx, scope.path+".compat.lock")
	cancelLock()
	if err != nil {
		return err
	}
	defer release()
	return m.ensureOpenAndReconcileLocked(scope)
}

func (m *topicStateManager) ensureOpenAndReconcileLocked(scope *topicStateScope) error {
	ctx := context.Background()
	if scope.store == nil {
		store, err := m.openTopicStoreWithRecovery(ctx, scope)
		if err != nil {
			return err
		}
		scope.store = store
	}
	dbSnapshot, err := scope.store.Snapshot(ctx)
	if err != nil {
		return err
	}
	// Publish a pending authoritative snapshot before inspecting legacy digests;
	// otherwise a half-written mirror can look like an old-version edit and roll
	// metadata back into SQLite.
	if dbSnapshot.State.LegacyBridge && dbSnapshot.State.LegacyPendingRevision != 0 {
		if err := m.mirrorIfPendingLocked(ctx, scope); err != nil {
			// SQLite remains readable and authoritative. Do not reconcile the
			// known-partial JSON snapshot, and do not divert a following mutation
			// into the legacy-only fallback path.
			slog.Warn("desktop: topic legacy mirror still pending", "scope", topicScopeKind(scope.root), "error_type", topicStateErrorType(err))
			return nil
		}
		dbSnapshot, err = scope.store.Snapshot(ctx)
		if err != nil {
			return err
		}
	}
	legacy, err := readLegacyTopicSnapshot(scope.root)
	if err != nil {
		return err
	}
	if legacy.exists && !dbSnapshot.State.LegacyBridge {
		if _, err := scope.store.SetLegacyBridge(ctx); err != nil {
			return err
		}
		dbSnapshot, err = scope.store.Snapshot(ctx)
		if err != nil {
			return err
		}
	}
	if legacy.exists && !legacyDigestsMatch(dbSnapshot.State, legacy.digests) {
		merged := mergeLegacyTopicSnapshot(dbSnapshot.Records, legacy, deletedTopicSet())
		if _, err := scope.store.ReplaceAll(ctx, merged); err != nil {
			return err
		}
	}
	if err := m.mirrorIfPendingLocked(ctx, scope); err != nil {
		// Migration/reconciliation already committed to SQLite. Keep the outbox
		// pending and let this access use the authoritative store.
		slog.Warn("desktop: topic legacy mirror pending after reconcile", "scope", topicScopeKind(scope.root), "error_type", topicStateErrorType(err))
	}
	return nil
}

func (m *topicStateManager) mirrorIfPendingLocked(ctx context.Context, scope *topicStateScope) error {
	snapshot, err := scope.store.Snapshot(ctx)
	if err != nil {
		return err
	}
	if !snapshot.State.LegacyBridge {
		return nil
	}
	deleted := deletedTopicSet()
	pruned := false
	for topicID := range deleted {
		if _, ok := snapshot.Records[topicID]; ok {
			delete(snapshot.Records, topicID)
			pruned = true
		}
	}
	if pruned {
		if _, err := scope.store.ReplaceAll(ctx, snapshot.Records); err != nil {
			return err
		}
		snapshot, err = scope.store.Snapshot(ctx)
		if err != nil {
			return err
		}
	}
	if snapshot.State.LegacyPendingRevision == 0 {
		legacy, err := readLegacyTopicSnapshot(scope.root)
		if err != nil {
			return err
		}
		if legacyDigestsMatch(snapshot.State, legacy.digests) {
			return nil
		}
	}
	digests, err := writeLegacyTopicSnapshot(scope.root, snapshot.Records)
	if err != nil {
		return err
	}
	_, err = scope.store.MarkLegacyExported(ctx, snapshot.State.Revision, digests)
	return err
}

func (m *topicStateManager) setTitle(workspaceRoot, topicID, title, source string) error {
	topicID, title, source = strings.TrimSpace(topicID), strings.TrimSpace(title), strings.TrimSpace(source)
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.Update(ctx, topicID, func(record *topicstate.Record) {
			applyTopicTitle(record, title, source)
		})
	}, func() error { return setLegacyTopicTitle(workspaceRoot, topicID, title, source) })
}

func (m *topicStateManager) setCreatedAt(workspaceRoot, topicID string, createdAt int64) error {
	topicID = strings.TrimSpace(topicID)
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.Update(ctx, topicID, func(record *topicstate.Record) { record.CreatedAtMS = createdAt })
	}, func() error { return setLegacyTopicCreatedAt(workspaceRoot, topicID, createdAt) })
}

func (m *topicStateManager) setAutoMeta(workspaceRoot, topicID string, value *topicAutoTitleMeta) error {
	topicID = strings.TrimSpace(topicID)
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.Update(ctx, topicID, func(record *topicstate.Record) {
			if value == nil {
				record.AutoMeta = nil
				return
			}
			record.AutoMeta = mergeKnownAutoMeta(record.AutoMeta, *value)
		})
	}, func() error { return setLegacyTopicAutoMeta(workspaceRoot, topicID, value) })
}

func (m *topicStateManager) delete(workspaceRoot, topicID string) error {
	topicID = strings.TrimSpace(topicID)
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.Delete(ctx, topicID)
	}, func() error { return deleteLegacyTopicState(workspaceRoot, topicID) })
}

func (m *topicStateManager) replaceTitles(workspaceRoot string, values map[string]string) error {
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.ReplaceTitles(ctx, values)
	}, func() error { return writeLegacyStringMap(workspaceRoot, topicTitlesPath(workspaceRoot), values) })
}

func (m *topicStateManager) replaceSources(workspaceRoot string, values map[string]string) error {
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.ReplaceSources(ctx, values)
	}, func() error { return writeLegacyStringMap(workspaceRoot, topicTitleSourcesPath(workspaceRoot), values) })
}

func (m *topicStateManager) mergeMissingTitleIndex(workspaceRoot string, titles, sources map[string]string) error {
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.MergeMissingTitleIndex(ctx, titles, sources, deletedTopicSet())
	}, func() error {
		return mergeLegacyMissingTitleIndex(workspaceRoot, titles, sources)
	})
}

func (m *topicStateManager) replaceCreatedAts(workspaceRoot string, values map[string]int64) error {
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.ReplaceCreatedAts(ctx, values)
	}, func() error { return writeLegacyInt64Map(workspaceRoot, topicCreatedAtsPath(workspaceRoot), values) })
}

func loadTopicTitles(workspaceRoot string) map[string]string {
	values := map[string]string{}
	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		legacy, legacyErr := loadLegacyStringMap(topicTitlesPath(workspaceRoot))
		logTopicStateReadFallback(workspaceRoot, err, legacyErr, legacyTopicFilesExist(workspaceRoot))
		for id, title := range legacy {
			values[id] = agent.UserPreviewText(title)
		}
		return values
	}
	for id, record := range snapshot.Records {
		if record.Title != "" {
			values[id] = agent.UserPreviewText(record.Title)
		}
	}
	return values
}

func loadTopicTitleSources(workspaceRoot string) map[string]string {
	values := map[string]string{}
	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		legacy, legacyErr := loadLegacyStringMap(topicTitleSourcesPath(workspaceRoot))
		logTopicStateReadFallback(workspaceRoot, err, legacyErr, legacyTopicFilesExist(workspaceRoot))
		return legacy
	}
	for id, record := range snapshot.Records {
		if record.TitleSource != "" {
			values[id] = record.TitleSource
		}
	}
	return values
}

func loadTopicCreatedAts(workspaceRoot string) map[string]int64 {
	values := map[string]int64{}
	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		legacy, legacyErr := loadLegacyInt64Map(topicCreatedAtsPath(workspaceRoot))
		logTopicStateReadFallback(workspaceRoot, err, legacyErr, legacyTopicFilesExist(workspaceRoot))
		return legacy
	}
	for id, record := range snapshot.Records {
		if record.CreatedAtMS > 0 {
			values[id] = record.CreatedAtMS
		}
	}
	return values
}

func loadTopicAutoTitleMeta(workspaceRoot string) map[string]topicAutoTitleMeta {
	values := map[string]topicAutoTitleMeta{}
	snapshot, err := desktopTopicState.snapshot(workspaceRoot)
	if err != nil {
		legacy, legacyErr := loadLegacyAutoMetaMap(topicAutoTitleMetaPath(workspaceRoot))
		logTopicStateReadFallback(workspaceRoot, err, legacyErr, legacyTopicFilesExist(workspaceRoot))
		return legacy
	}
	for id, record := range snapshot.Records {
		var value topicAutoTitleMeta
		if len(record.AutoMeta) > 0 && json.Unmarshal(record.AutoMeta, &value) == nil {
			values[id] = value
		}
	}
	return values
}

func saveTopicTitleIndex(workspaceRoot string, titles, sources map[string]string) error {
	if titles == nil {
		titles = loadTopicTitles(workspaceRoot)
	}
	if sources == nil {
		sources = loadTopicTitleSources(workspaceRoot)
	}
	return desktopTopicState.mergeMissingTitleIndex(workspaceRoot, titles, sources)
}

func readLegacyTopicSnapshot(workspaceRoot string) (legacyTopicSnapshot, error) {
	snapshot := legacyTopicSnapshot{
		titles: map[string]string{}, sources: map[string]string{},
		createdAts: map[string]int64{}, autoMeta: map[string]json.RawMessage{},
	}
	paths := legacyTopicPaths(workspaceRoot)
	for index, path := range paths {
		data, err := readFileUTF8(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return snapshot, err
		}
		snapshot.exists = true
		snapshot.digests[index] = digestBytes(data)
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(data, &entries); err != nil || entries == nil {
			if err == nil {
				err = errors.New("legacy topic file is not an object")
			}
			return snapshot, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
		}
		for topicID, raw := range entries {
			topicID = strings.TrimSpace(topicID)
			if topicID == "" {
				continue
			}
			switch index {
			case 0:
				var value string
				if json.Unmarshal(raw, &value) == nil {
					snapshot.titles[topicID] = agent.UserPreviewText(value)
				}
			case 1:
				var value string
				if json.Unmarshal(raw, &value) == nil {
					snapshot.sources[topicID] = strings.TrimSpace(value)
				}
			case 2:
				var value int64
				if json.Unmarshal(raw, &value) == nil && value > 0 {
					snapshot.createdAts[topicID] = value
				}
			case 3:
				var object map[string]json.RawMessage
				if json.Unmarshal(raw, &object) == nil && object != nil {
					snapshot.autoMeta[topicID] = append(json.RawMessage(nil), raw...)
				}
			}
		}
	}
	return snapshot, nil
}

func mergeLegacyTopicSnapshot(current map[string]topicstate.Record, legacy legacyTopicSnapshot, deleted map[string]bool) map[string]topicstate.Record {
	merged := make(map[string]topicstate.Record, len(current)+len(legacy.titles))
	for id, record := range current {
		if !deleted[id] {
			merged[id] = record
		}
	}
	ids := map[string]bool{}
	for id := range legacy.titles {
		ids[id] = true
	}
	for id := range legacy.sources {
		ids[id] = true
	}
	for id := range legacy.createdAts {
		ids[id] = true
	}
	for id := range legacy.autoMeta {
		ids[id] = true
	}
	for id := range ids {
		if deleted[id] {
			delete(merged, id)
			continue
		}
		record := merged[id]
		record.TopicID = id
		legacySource := strings.TrimSpace(legacy.sources[id])
		legacyTitle := strings.TrimSpace(legacy.titles[id])
		if legacyTitle != "" && (legacySource != topicTitleSourceAuto || record.TitleSource != topicTitleSourceManual) {
			record.Title = legacyTitle
		}
		if legacySource != "" && !(legacySource == topicTitleSourceAuto && record.TitleSource == topicTitleSourceManual) {
			record.TitleSource = legacySource
		}
		if createdAt := legacy.createdAts[id]; createdAt > 0 {
			record.CreatedAtMS = createdAt
		}
		if raw := legacy.autoMeta[id]; len(raw) > 0 {
			record.AutoMeta = mergeLegacyAutoMeta(record.AutoMeta, raw)
		}
		merged[id] = record
	}
	return merged
}

func writeLegacyTopicSnapshot(workspaceRoot string, records map[string]topicstate.Record) ([4]string, error) {
	var digests [4]string
	titles := map[string]string{}
	sources := map[string]string{}
	created := map[string]int64{}
	auto := map[string]json.RawMessage{}
	for id, record := range records {
		if strings.TrimSpace(record.Title) != "" {
			titles[id] = record.Title
		}
		if strings.TrimSpace(record.TitleSource) != "" {
			sources[id] = record.TitleSource
		}
		if record.CreatedAtMS > 0 {
			created[id] = record.CreatedAtMS
		}
		if len(record.AutoMeta) > 0 {
			auto[id] = append(json.RawMessage(nil), record.AutoMeta...)
		}
	}
	values := []any{titles, sources, created, auto}
	for index, path := range legacyTopicPaths(workspaceRoot) {
		data, err := json.MarshalIndent(values[index], "", "  ")
		if err != nil {
			return digests, err
		}
		if err := ensureLegacyTopicStateDir(workspaceRoot, path); err != nil {
			return digests, err
		}
		if topicLegacyWriteHookForTest != nil {
			if err := topicLegacyWriteHookForTest(path); err != nil {
				return digests, err
			}
		}
		if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
			return digests, err
		}
		digests[index] = digestBytes(data)
	}
	return digests, nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func legacyDigestsMatch(state topicstate.State, digests [4]string) bool {
	return state.LegacyTitlesDigest == digests[0] &&
		state.LegacySourcesDigest == digests[1] &&
		state.LegacyCreatedAtsDigest == digests[2] &&
		state.LegacyAutoMetaDigest == digests[3]
}

func deletedTopicSet() map[string]bool {
	deleted := loadProjectsFile().DeletedTopics
	set := make(map[string]bool, len(deleted))
	for _, topicID := range deleted {
		set[strings.TrimSpace(topicID)] = true
	}
	return set
}

func mergeKnownAutoMeta(existing json.RawMessage, value topicAutoTitleMeta) json.RawMessage {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(existing, &fields)
	known, _ := json.Marshal(value)
	var knownFields map[string]json.RawMessage
	_ = json.Unmarshal(known, &knownFields)
	for _, key := range []string{"stage", "userTurns", "basisHash", "updatedAt"} {
		delete(fields, key)
	}
	maps.Copy(fields, knownFields)
	data, _ := json.Marshal(fields)
	return data
}

func ensureLegacyTopicStateDir(workspaceRoot, path string) error {
	if root := strings.TrimSpace(workspaceRoot); root != "" && !existingDirectory(root) {
		return fmt.Errorf("workspace root %q no longer exists", root)
	}
	return os.MkdirAll(filepath.Dir(path), 0o700)
}

func writeLegacyStringMap(workspaceRoot, path string, values map[string]string) error {
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := ensureLegacyTopicStateDir(workspaceRoot, path); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func writeLegacyInt64Map(workspaceRoot, path string, values map[string]int64) error {
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := ensureLegacyTopicStateDir(workspaceRoot, path); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func writeLegacyAutoMetaMap(workspaceRoot string, values map[string]topicAutoTitleMeta) error {
	existing, err := loadLegacyRawAutoMetaMap(topicAutoTitleMetaPath(workspaceRoot))
	if err != nil {
		return err
	}
	rawValues := make(map[string]json.RawMessage, len(values))
	for id, value := range values {
		rawValues[id] = mergeKnownAutoMeta(existing[id], value)
	}
	data, err := json.MarshalIndent(rawValues, "", "  ")
	if err != nil {
		return err
	}
	path := topicAutoTitleMetaPath(workspaceRoot)
	if err := ensureLegacyTopicStateDir(workspaceRoot, path); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func setLegacyTopicTitle(workspaceRoot, topicID, title, source string) error {
	titles, err := loadLegacyStringMap(topicTitlesPath(workspaceRoot))
	if err != nil {
		return err
	}
	sources, err := loadLegacyStringMap(topicTitleSourcesPath(workspaceRoot))
	if err != nil {
		return err
	}
	if title == "" {
		delete(titles, topicID)
		delete(sources, topicID)
	} else {
		titles[topicID] = title
		if source == "" {
			delete(sources, topicID)
		} else {
			sources[topicID] = source
		}
	}
	if err := writeLegacyStringMap(workspaceRoot, topicTitlesPath(workspaceRoot), titles); err != nil {
		return err
	}
	if err := writeLegacyStringMap(workspaceRoot, topicTitleSourcesPath(workspaceRoot), sources); err != nil {
		return err
	}
	if source == topicTitleSourceManual || (source == topicTitleSourceAuto && isDefaultTopicTitle(title)) {
		return setLegacyTopicAutoMeta(workspaceRoot, topicID, nil)
	}
	return nil
}

func setLegacyTopicCreatedAt(workspaceRoot, topicID string, createdAt int64) error {
	values, err := loadLegacyInt64Map(topicCreatedAtsPath(workspaceRoot))
	if err != nil {
		return err
	}
	if topicID == "" || createdAt <= 0 {
		delete(values, topicID)
	} else {
		values[topicID] = createdAt
	}
	return writeLegacyInt64Map(workspaceRoot, topicCreatedAtsPath(workspaceRoot), values)
}

func setLegacyTopicAutoMeta(workspaceRoot, topicID string, value *topicAutoTitleMeta) error {
	values, err := loadLegacyRawAutoMetaMap(topicAutoTitleMetaPath(workspaceRoot))
	if err != nil {
		return err
	}
	if value == nil {
		delete(values, topicID)
	} else {
		values[topicID] = mergeKnownAutoMeta(values[topicID], *value)
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	path := topicAutoTitleMetaPath(workspaceRoot)
	if err := ensureLegacyTopicStateDir(workspaceRoot, path); err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func deleteLegacyTopicState(workspaceRoot, topicID string) error {
	titles, err := loadLegacyStringMap(topicTitlesPath(workspaceRoot))
	if err != nil {
		return err
	}
	sources, err := loadLegacyStringMap(topicTitleSourcesPath(workspaceRoot))
	if err != nil {
		return err
	}
	created, err := loadLegacyInt64Map(topicCreatedAtsPath(workspaceRoot))
	if err != nil {
		return err
	}
	auto, err := loadLegacyAutoMetaMap(topicAutoTitleMetaPath(workspaceRoot))
	if err != nil {
		return err
	}
	delete(titles, topicID)
	delete(sources, topicID)
	delete(created, topicID)
	delete(auto, topicID)
	if err := writeLegacyStringMap(workspaceRoot, topicTitlesPath(workspaceRoot), titles); err != nil {
		return err
	}
	if err := writeLegacyStringMap(workspaceRoot, topicTitleSourcesPath(workspaceRoot), sources); err != nil {
		return err
	}
	if err := writeLegacyInt64Map(workspaceRoot, topicCreatedAtsPath(workspaceRoot), created); err != nil {
		return err
	}
	return writeLegacyAutoMetaMap(workspaceRoot, auto)
}

func loadLegacyStringMap(path string) (map[string]string, error) {
	values := map[string]string{}
	data, err := readFileUTF8(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &values); err != nil || values == nil {
		return map[string]string{}, nil
	}
	return values, nil
}

func loadLegacyInt64Map(path string) (map[string]int64, error) {
	values := map[string]int64{}
	data, err := readFileUTF8(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &values); err != nil || values == nil {
		return map[string]int64{}, nil
	}
	return values, nil
}

func loadLegacyAutoMetaMap(path string) (map[string]topicAutoTitleMeta, error) {
	values := map[string]topicAutoTitleMeta{}
	data, err := readFileUTF8(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &values); err != nil || values == nil {
		return map[string]topicAutoTitleMeta{}, nil
	}
	return values, nil
}

func loadLegacyRawAutoMetaMap(path string) (map[string]json.RawMessage, error) {
	values := map[string]json.RawMessage{}
	data, err := readFileUTF8(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &values); err != nil || values == nil {
		return map[string]json.RawMessage{}, nil
	}
	return values, nil
}

func legacyFallbackAllowed(err error) bool {
	var future *topicstate.FutureSchemaError
	return !errors.As(err, &future)
}
