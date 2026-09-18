package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/fileutil"
	filelock "reasonix/internal/identitylock"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

type MigrationMapping struct {
	SchemaVersion int              `json:"schemaVersion"`
	Entries       []MigrationEntry `json:"entries"`
}

type MigrationEntry struct {
	SourcePath   string    `json:"sourcePath"`
	SourceSize   int64     `json:"sourceSize"`
	SourceSHA256 string    `json:"sourceSha256"`
	LegacyHeadID string    `json:"legacyHeadId,omitempty"`
	TargetCodec  string    `json:"targetCodec"`
	TargetID     string    `json:"targetId"`
	CreatedAt    time.Time `json:"createdAt"`
}

type MigrationResult struct {
	TargetID   string
	TargetDir  string
	Source     Source
	Reused     bool
	MessageNum int
}

var migrationMu sync.Mutex

// MigrateLegacy freezes one legacy session under its write lease, constructs a
// complete canonical directory in a sibling temporary directory, then publishes it by
// rename. Original artifacts are copied byte-for-byte under legacy/ and are
// never rewritten or removed.
func MigrateLegacy(ctx context.Context, sourcePath, targetRoot string) (MigrationResult, error) {
	return MigrateLegacyHead(ctx, sourcePath, targetRoot, "")
}

// MigrateLegacyHead turns one reachable legacy DAG head into its own canonical
// session. An empty head ID imports the legacy file's selected/default view.
// Head identity participates in both the deterministic target ID and migration
// map key, so continuing two old heads can never merge their future writes.
func MigrateLegacyHead(ctx context.Context, sourcePath, targetRoot, legacyHeadID string) (MigrationResult, error) {
	return migrateLegacyHead(ctx, sourcePath, targetRoot, legacyHeadID, false)
}

// migrateLegacyHeadForHost permits an existing host transition to freeze a
// legacy source already leased by this process. Cross-process ownership is
// still enforced by the OS lease. Callers must serialize the transition with
// their host/session gate and stop the old producer before invoking it.
func migrateLegacyHeadForHost(ctx context.Context, sourcePath, targetRoot, legacyHeadID string) (MigrationResult, error) {
	return migrateLegacyHead(ctx, sourcePath, targetRoot, legacyHeadID, true)
}

func migrateLegacyHead(ctx context.Context, sourcePath, targetRoot, legacyHeadID string, allowCurrentOwner bool) (MigrationResult, error) {
	targetRoot = filepath.Clean(strings.TrimSpace(targetRoot))
	if targetRoot == "." {
		return MigrationResult{}, fmt.Errorf("session: source and target root are required")
	}
	frozen, err := freezeLegacyHead(ctx, sourcePath, legacyHeadID, allowCurrentOwner)
	if err != nil {
		return MigrationResult{}, err
	}
	return frozen.publish(ctx, targetRoot, CreateOptions{})
}

// frozenLegacyHead is one legacy head reduced to an immutable, already-parsed
// migration input. Nothing is published while it is being built, so a caller
// that must compare it against a paired event sidecar can still refuse the
// import without leaving a partially-adopted target behind.
type frozenLegacyHead struct {
	sourcePath           string
	headID               string
	source               Source
	artifacts            []frozenArtifact
	targetID             string
	messageSpool         string
	messageCount         int
	modelRef             string
	modelIdentity        string
	modelMessages        []provider.Message
	projectionDiagnostic string
	goal                 map[string]any
	freezeDir            string
}

// LegacyMigrationHeads lists all heads from an immutable copy, without changing
// source selection, caches, or logs. Retired heads are returned for the caller
// to distinguish deliberate deletion from a missing branch.
func LegacyMigrationHeads(ctx context.Context, sourcePath string) ([]agent.SessionHead, error) {
	sourcePath = agent.CanonicalSessionPath(sourcePath)
	lease, err := agent.TryAcquireSessionLease(sourcePath)
	if err != nil {
		return nil, err
	}
	artifacts, _, dir, err := freezeLegacyArtifacts(ctx, sourcePath)
	lease.Release()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	for _, artifact := range artifacts {
		if artifact.path == sourcePath {
			return agent.ListSessionHeadsForMigration(ctx, artifact.frozenPath)
		}
	}
	return nil, os.ErrNotExist
}

// freezeLegacyHead acquires the source lease, copies every durable artifact
// byte-for-byte, then parses only the frozen copy. The lease is released before
// parsing, which is safe precisely because the parse never reads the original.
func freezeLegacyHead(ctx context.Context, sourcePath, legacyHeadID string, allowCurrentOwner bool) (*frozenLegacyHead, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sourcePath = agent.CanonicalSessionPath(sourcePath)
	legacyHeadID = strings.TrimSpace(legacyHeadID)
	if sourcePath == "" {
		return nil, fmt.Errorf("session: source is required")
	}
	var lease *agent.SessionLease
	if !allowCurrentOwner || !agent.SessionLeaseHeldByCurrentRuntime(sourcePath) {
		acquired, acquireErr := agent.TryAcquireSessionLease(sourcePath)
		if acquireErr != nil {
			return nil, fmt.Errorf("freeze legacy session: %w", acquireErr)
		}
		lease = acquired
	}
	artifacts, source, freezeDir, err := freezeLegacyArtifacts(ctx, sourcePath)
	if lease != nil {
		lease.Release()
	}
	if err != nil {
		return nil, err
	}
	source.LegacyHeadID = legacyHeadID
	parsed, err := parseFrozenLegacy(ctx, artifacts, sourcePath, legacyHeadID)
	if err != nil {
		_ = os.RemoveAll(freezeDir)
		return nil, err
	}
	// The branch-metadata sidecar carries the source's own created/updated
	// times; the source file mtime is the fallback when it is absent.
	if !parsed.createdAt.IsZero() {
		source.CreatedAt = parsed.createdAt
	}
	if !parsed.updatedAt.IsZero() {
		source.UpdatedAt = parsed.updatedAt
	}
	return &frozenLegacyHead{
		sourcePath: sourcePath, headID: legacyHeadID, source: source, artifacts: artifacts,
		targetID:     migrationTargetID(sourcePath, source.SHA256, legacyHeadID),
		messageSpool: parsed.messageSpool, messageCount: parsed.messageCount,
		modelRef: parsed.modelRef, modelIdentity: parsed.modelIdentity,
		modelMessages: parsed.modelMessages, projectionDiagnostic: parsed.projectionDiagnostic,
		goal:      parsed.goal,
		freezeDir: freezeDir,
	}, nil
}

type frozenLegacyParse struct {
	messageSpool         string
	messageCount         int
	modelRef             string
	modelIdentity        string
	modelMessages        []provider.Message
	projectionDiagnostic string
	goal                 map[string]any
	// createdAt and updatedAt are the source session's own times, read from the
	// legacy branch-metadata sidecar so the migrated target keeps its place in
	// the timeline instead of sorting by import time.
	createdAt time.Time
	updatedAt time.Time
}

// parseFrozenLegacy reads the frozen artifacts from a private directory so the
// published target can never depend on bytes outside the frozen input.
func parseFrozenLegacy(ctx context.Context, artifacts []frozenArtifact, sourcePath, legacyHeadID string) (frozenLegacyParse, error) {
	if len(artifacts) == 0 {
		return frozenLegacyParse{}, os.ErrNotExist
	}
	if err := ctx.Err(); err != nil {
		return frozenLegacyParse{}, err
	}
	frozenSourcePath := ""
	for _, artifact := range artifacts {
		if artifact.path == sourcePath {
			frozenSourcePath = artifact.frozenPath
			break
		}
	}
	if frozenSourcePath == "" {
		return frozenLegacyParse{}, os.ErrNotExist
	}
	messageSpool := filepath.Join(filepath.Dir(frozenSourcePath), ".migration-messages.jsons")
	spool, err := os.OpenFile(messageSpool, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		return frozenLegacyParse{}, err
	}
	encoder := json.NewEncoder(spool)
	reset := func() error {
		if err := spool.Truncate(0); err != nil {
			return err
		}
		_, err := spool.Seek(0, io.SeekStart)
		return err
	}
	emit := func(message provider.Message) error { return encoder.Encode(message) }
	stream, streamErr := agent.StreamSessionMessagesForMigration(ctx, frozenSourcePath, legacyHeadID, reset, emit)
	if streamErr == nil {
		streamErr = spool.Sync()
	}
	closeErr := spool.Close()
	if streamErr != nil {
		return frozenLegacyParse{}, fmt.Errorf("read legacy transcript: %w", streamErr)
	}
	if closeErr != nil {
		return frozenLegacyParse{}, closeErr
	}
	parsed := frozenLegacyParse{messageSpool: messageSpool, messageCount: stream.Messages}
	if _, err := os.Stat(agent.ContextStatePath(frozenSourcePath)); err == nil {
		canonical, loadErr := readMigrationMessageSpool(ctx, messageSpool)
		if loadErr != nil {
			return frozenLegacyParse{}, loadErr
		}
		modelMessages, valid, projectionErr := agent.LoadValidContextProjectionForMigration(frozenSourcePath, canonical)
		switch {
		case projectionErr != nil:
			parsed.projectionDiagnostic = "legacy context projection ignored: " + projectionErr.Error()
		case valid:
			parsed.modelMessages = provider.ModelMessages(modelMessages)
		default:
			parsed.projectionDiagnostic = "legacy context projection ignored: sidecar does not match canonical history"
		}
	} else if !os.IsNotExist(err) {
		return frozenLegacyParse{}, err
	}
	if modelRef, modelIdentity, ok := agent.LoadSessionModelSelection(frozenSourcePath); ok && strings.TrimSpace(modelRef) != "" {
		parsed.modelRef, parsed.modelIdentity = strings.TrimSpace(modelRef), strings.TrimSpace(modelIdentity)
	}
	if meta, ok, metaErr := agent.LoadBranchMeta(frozenSourcePath); metaErr == nil && ok {
		parsed.createdAt, parsed.updatedAt = meta.CreatedAt, meta.UpdatedAt
	}
	parsed.goal = sanitizedLegacyGoal(frozenSourcePath)
	return parsed, nil
}

func readMigrationMessageSpool(ctx context.Context, path string) ([]provider.Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(&contextReader{ctx: ctx, reader: file})
	var messages []provider.Message
	for {
		var message provider.Message
		if err := decoder.Decode(&message); errors.Is(err, io.EOF) {
			return messages, nil
		} else if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
}

// publish materializes the frozen input as the deterministic final target. The
// directory is built in a sibling temporary path and atomically renamed, so a
// reader never observes a partial session.
func (f *frozenLegacyHead) publish(ctx context.Context, targetRoot string, options CreateOptions) (MigrationResult, error) {
	if f == nil {
		return MigrationResult{}, fmt.Errorf("session: nil frozen legacy head")
	}
	defer os.RemoveAll(f.freezeDir)
	targetDir := filepath.Join(targetRoot, f.targetID)
	result := MigrationResult{TargetID: f.targetID, TargetDir: targetDir, Source: f.source, MessageNum: f.messageCount}

	migrationMu.Lock()
	defer migrationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return MigrationResult{}, err
	}
	reused, err := f.reusePublished(ctx, targetRoot, targetDir, options)
	if err != nil {
		return MigrationResult{}, err
	}
	if reused {
		result.Reused = true
		return result, nil
	}
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		return MigrationResult{}, err
	}
	tmp, err := os.MkdirTemp(targetRoot, "."+f.targetID+".tmp-")
	if err != nil {
		return MigrationResult{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(tmp)
		}
	}()

	createdAt, updatedAt := sourceActivityTimes(f.source)
	manifest := Manifest{SchemaVersion: SchemaVersion, Codec: Codec, StorageRevision: StorageRevision, ContentRoot: sharedContentRoot, SessionID: f.targetID, CreatedAt: createdAt, Source: &f.source}
	if err := writeImportedManifest(tmp, manifest, options); err != nil {
		return MigrationResult{}, err
	}
	legacyDir := filepath.Join(tmp, "legacy")
	for _, artifact := range f.artifacts {
		if err := ctx.Err(); err != nil {
			return MigrationResult{}, err
		}
		if err := copyFrozenArtifact(ctx, artifact.frozenPath, filepath.Join(legacyDir, filepath.Base(artifact.path)), artifact.mode); err != nil {
			return MigrationResult{}, err
		}
	}

	target, err := OpenWithOptions(tmp, f.targetID, OpenOptions{ExternalHistory: true})
	if err != nil {
		return MigrationResult{}, err
	}
	appendErr := f.appendMessages(ctx, target)
	if appendErr == nil {
		appendErr = f.appendMetadata(ctx, target)
	}
	if appendErr == nil {
		_, appendErr = target.Flush(ctx)
	}
	closeErr := target.Close(ctx)
	if appendErr != nil {
		return MigrationResult{}, appendErr
	}
	if closeErr != nil {
		return MigrationResult{}, closeErr
	}
	if err := stampLogActivity(logPathForManifest(tmp, manifest), updatedAt); err != nil {
		return MigrationResult{}, err
	}
	if err := os.Rename(tmp, targetDir); err != nil {
		return MigrationResult{}, fmt.Errorf("publish canonical session: %w", err)
	}
	published = true
	if err := appendMigrationMapping(ctx, targetRoot, MigrationEntry{SourcePath: f.sourcePath, SourceSize: f.source.Size, SourceSHA256: f.source.SHA256, LegacyHeadID: f.headID, TargetCodec: Codec, TargetID: f.targetID, CreatedAt: time.Now().UTC()}); err != nil {
		// The target is already complete and deterministically discoverable. A
		// later retry repairs the mapping without rebuilding or resending work.
		return result, fmt.Errorf("publish migration mapping: %w", err)
	}
	return result, nil
}

func (f *frozenLegacyHead) appendMessages(ctx context.Context, target *Session) (err error) {
	const messageBatchSize = 128
	spool, err := os.Open(f.messageSpool)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, spool.Close()) }()

	decoder := json.NewDecoder(&contextReader{ctx: ctx, reader: spool})
	events := make([]Event, 0, messageBatchSize)
	batchNumber := 0
	flush := func() error {
		if len(events) == 0 {
			return nil
		}
		_, err := target.Append(ctx, Batch{
			OperationID: fmt.Sprintf("legacy-import:%s:messages:%d", f.source.SHA256, batchNumber),
			Events:      events,
		})
		batchNumber++
		events = make([]Event, 0, messageBatchSize)
		return err
	}
	for {
		var message provider.Message
		decodeErr := decoder.Decode(&message)
		if errors.Is(decodeErr, io.EOF) {
			return flush()
		}
		if decodeErr != nil {
			return decodeErr
		}
		raw, marshalErr := json.Marshal(struct {
			Message provider.Message `json:"message"`
		}{Message: message})
		if marshalErr != nil {
			return marshalErr
		}
		events = append(events, Event{Kind: "message/complete", Payload: raw})
		if len(events) == messageBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

func (f *frozenLegacyHead) appendMetadata(ctx context.Context, target *Session) error {
	prefix := "legacy-import:" + f.source.SHA256 + ":"
	if len(f.modelMessages) > 0 {
		event, err := legacyModelContextEvent(f.modelMessages)
		if err != nil {
			return err
		}
		if _, err := target.Append(ctx, Batch{OperationID: prefix + "model-context", Events: []Event{event}}); err != nil {
			return err
		}
	}
	if f.projectionDiagnostic != "" {
		raw, err := json.Marshal(map[string]string{
			"code":   "legacy_context_projection_ignored",
			"detail": f.projectionDiagnostic,
		})
		if err != nil {
			return err
		}
		if _, err := target.Append(ctx, Batch{OperationID: prefix + "projection-diagnostic", Events: []Event{{Kind: "diagnostic", Optional: true, Payload: raw}}}); err != nil {
			return err
		}
	}
	if f.modelRef != "" {
		raw, err := json.Marshal(map[string]string{"modelRef": f.modelRef, "modelIdentity": f.modelIdentity})
		if err != nil {
			return err
		}
		if _, err := target.Append(ctx, Batch{OperationID: prefix + "model", Events: []Event{{Kind: "session/config", Payload: raw}}}); err != nil {
			return err
		}
	}
	if f.goal == nil {
		return nil
	}
	raw, err := json.Marshal(f.goal)
	if err != nil {
		return err
	}
	_, err = target.Append(ctx, Batch{OperationID: prefix + "goal", Events: []Event{{Kind: "goal/state", Payload: raw}}})
	return err
}

func (f *frozenLegacyHead) reusePublished(ctx context.Context, targetRoot, targetDir string, options CreateOptions) (bool, error) {
	manifest, err := readManifest(filepath.Join(targetDir, "manifest.json"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if manifest.Source == nil || manifest.Source.Path != f.sourcePath || manifest.Source.SHA256 != f.source.SHA256 || manifest.Source.LegacyHeadID != f.headID {
		return false, fmt.Errorf("session: target %s already exists for different input", f.targetID)
	}
	if err := validateSessionHeaderForCreate(targetDir, f.targetID, options); err != nil {
		return false, err
	}
	if err := f.repairPublishedProjection(ctx, targetDir); err != nil {
		return false, err
	}
	entry := MigrationEntry{SourcePath: f.sourcePath, SourceSize: f.source.Size, SourceSHA256: f.source.SHA256, LegacyHeadID: f.headID, TargetCodec: Codec, TargetID: f.targetID, CreatedAt: manifest.CreatedAt}
	if err := appendMigrationMapping(ctx, targetRoot, entry); err != nil {
		return false, fmt.Errorf("repair migration mapping: %w", err)
	}
	return true, nil
}

func legacyModelContextEvent(messages []provider.Message) (Event, error) {
	raw, err := json.Marshal(map[string]any{
		"messages": provider.ModelMessages(messages),
		"reason":   "legacy-import-projection",
	})
	if err != nil {
		return Event{}, err
	}
	return Event{Kind: "model/context-replace", Payload: raw}, nil
}

// repairPublishedProjection upgrades a target created by an older importer
// only while every commit still belongs to the deterministic import. Any
// subsequent user or runtime commit makes the target ineligible for mutation.
func (f *frozenLegacyHead) repairPublishedProjection(ctx context.Context, targetDir string) error {
	if f == nil || len(f.modelMessages) == 0 {
		return nil
	}
	prefix := "legacy-import:" + f.source.SHA256 + ":"
	pristine, hasProjection := true, false
	if err := VisitCommits(ctx, targetDir, func(commit Commit) error {
		if !strings.HasPrefix(commit.OperationID, prefix) {
			pristine = false
		}
		for _, event := range commit.Events {
			if event.Kind == "model/context-replace" || event.Kind == "compaction" {
				hasProjection = true
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if !pristine || hasProjection {
		return nil
	}
	target, err := OpenWithOptions(targetDir, f.targetID, OpenOptions{ExternalHistory: true})
	if err != nil {
		// A live canonical target owns its writer lease. Reuse remains safe, but
		// an in-place repair must wait for a later inactive retry.
		return nil
	}
	event, err := legacyModelContextEvent(f.modelMessages)
	if err == nil {
		_, err = target.Append(ctx, Batch{OperationID: prefix + "model-context", Events: []Event{event}})
	}
	if err == nil {
		_, err = target.Flush(ctx)
	}
	return errors.Join(err, target.Close(ctx))
}

func writeImportedManifest(dir string, manifest Manifest, options CreateOptions) error {
	if err := writeManifest(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		return err
	}
	return writeSessionHeaderForCreate(dir, manifest.SessionID, manifest.CreatedAt, options)
}

type frozenArtifact struct {
	path       string
	frozenPath string
	mode       fs.FileMode
	size       int64
}

func freezeLegacyArtifacts(ctx context.Context, sourcePath string) ([]frozenArtifact, Source, string, error) {
	freezeDir, err := os.MkdirTemp("", "reasonix-legacy-freeze-")
	if err != nil {
		return nil, Source{}, "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(freezeDir)
		}
	}()
	paths := append([]string{sourcePath}, store.SessionSidecarFiles(sourcePath)...)
	seen := map[string]bool{}
	artifacts := []frozenArtifact{}
	foundSource := false
	var sourceModTime time.Time
	hash := sha256.New()
	var total int64
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, Source{}, "", err
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, Source{}, "", err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		frozenPath := filepath.Join(freezeDir, filepath.Base(path))
		if err := copyFrozenArtifact(ctx, path, frozenPath, info.Mode().Perm()); err != nil {
			return nil, Source{}, "", err
		}
		artifacts = append(artifacts, frozenArtifact{path: path, frozenPath: frozenPath, mode: info.Mode().Perm(), size: info.Size()})
		if path == sourcePath {
			foundSource = true
			sourceModTime = info.ModTime()
		}
	}
	if !foundSource {
		return nil, Source{}, "", os.ErrNotExist
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].path < artifacts[j].path })
	for _, artifact := range artifacts {
		name := filepath.Base(artifact.path)
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		file, err := os.Open(artifact.frozenPath)
		if err != nil {
			return nil, Source{}, "", err
		}
		_, copyErr := copyStreamWithContext(ctx, hash, file)
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			return nil, Source{}, "", errors.Join(copyErr, closeErr)
		}
		hash.Write([]byte{0})
		total += artifact.size
	}
	keep = true
	return artifacts, Source{
		Path: sourcePath, Size: total, SHA256: hex.EncodeToString(hash.Sum(nil)), Version: "legacy",
		CreatedAt: sourceModTime, UpdatedAt: sourceModTime,
	}, freezeDir, nil
}

func copyFrozenArtifact(ctx context.Context, source, target string, mode fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".frozen-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := copyStreamWithContext(ctx, tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return fileutil.ReplaceFile(tmpPath, target)
}

func copyStreamWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 1<<20)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func sanitizedLegacyGoal(sourcePath string) map[string]any {
	b, err := os.ReadFile(store.SessionGoalState(sourcePath))
	if err != nil {
		return nil
	}
	var goal map[string]any
	if json.Unmarshal(b, &goal) != nil {
		return nil
	}
	delete(goal, "todos")
	delete(goal, "todo")
	delete(goal, "auto_continue")
	delete(goal, "autoContinue")
	return goal
}

func migrationTargetID(path, digest, legacyHeadID string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + digest + "\x00" + legacyHeadID + "\x00" + Codec))
	return hex.EncodeToString(sum[:12])
}

func readManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if !currentStoredManifest(m) {
		return Manifest{}, fmt.Errorf("%w: manifest schema or codec", ErrUnsupportedVersion)
	}
	return m, nil
}

func writeManifest(path string, m Manifest) error {
	if m.Codec == "" {
		m.Codec = Codec
	}
	if m.Codec == Codec && m.SchemaVersion == SchemaVersion && m.StorageRevision == 0 {
		m.StorageRevision = StorageRevision
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, append(b, '\n'), 0o600)
}

func appendMigrationMapping(ctx context.Context, root string, entry MigrationEntry) error {
	path := filepath.Join(root, "migration-map.json")
	release, err := acquireMigrationMapLease(ctx, path)
	if err != nil {
		return fmt.Errorf("lock migration map: %w", err)
	}
	defer release()
	mapping := MigrationMapping{SchemaVersion: SchemaVersion, Entries: []MigrationEntry{}}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &mapping); err != nil {
			return err
		}
		if mapping.SchemaVersion != SchemaVersion {
			return fmt.Errorf("%w: migration map schema %d", ErrUnsupportedVersion, mapping.SchemaVersion)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, existing := range mapping.Entries {
		if existing.SourcePath == entry.SourcePath && existing.SourceSHA256 == entry.SourceSHA256 && existing.LegacyHeadID == entry.LegacyHeadID && existing.TargetCodec == entry.TargetCodec {
			return nil
		}
	}
	mapping.Entries = append(mapping.Entries, entry)
	sort.Slice(mapping.Entries, func(i, j int) bool {
		if mapping.Entries[i].SourcePath == mapping.Entries[j].SourcePath {
			if mapping.Entries[i].SourceSHA256 == mapping.Entries[j].SourceSHA256 {
				if mapping.Entries[i].LegacyHeadID == mapping.Entries[j].LegacyHeadID {
					return mapping.Entries[i].TargetCodec < mapping.Entries[j].TargetCodec
				}
				return mapping.Entries[i].LegacyHeadID < mapping.Entries[j].LegacyHeadID
			}
			return mapping.Entries[i].SourceSHA256 < mapping.Entries[j].SourceSHA256
		}
		return mapping.Entries[i].SourcePath < mapping.Entries[j].SourcePath
	})
	b, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, append(b, '\n'), 0o600)
}

func acquireMigrationMapLease(ctx context.Context, path string) (func(), error) {
	return filelock.Acquire(ctx, path+".lock")
}

func IsUnsupported(err error) bool { return errors.Is(err, ErrUnsupportedVersion) }
