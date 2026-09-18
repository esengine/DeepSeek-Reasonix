package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	bolt "go.etcd.io/bbolt"

	"reasonix/internal/agent"
	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

// Version 6 retains optional submission receipts in recovery checkpoints.
// Older projections are disposable and rebuild from the unchanged durable log.
const recoveryProjectionVersion = 6

const (
	recoveryFormatVersion = 1
	recoveryDBName        = "recovery-v1.bolt"
	storageIdentityName   = "storage.identity.json"
	RecentMessageLimit    = 100
	recentInlineBytes     = 32 << 10
	recentResponseBytes   = 512 << 10
	recentSnapshotName    = "recent-v1.json"
)

var (
	recoveryMetaBucket       = []byte("meta")
	recoveryCheckpointBucket = []byte("checkpoints")
	recoveryOperationBucket  = []byte("operations")
	recoveryCurrentKey       = []byte("current")
	recoveryPreviousKey      = []byte("previous")
)

// RecoveryOpenStats reports work performed by the normal open path. It is
// intentionally small and stable enough for capacity tests and host telemetry.
type RecoveryOpenStats struct {
	UsedCheckpoint bool  `json:"usedCheckpoint"`
	LogBytesRead   int64 `json:"logBytesRead"`
	LogBytesTotal  int64 `json:"logBytesTotal"`
	TailCommits    int   `json:"tailCommits"`
}

// RecentSnapshot is the bounded, read-only baseline used before history or
// search projections are available.
type RecentSnapshot struct {
	Version           int                 `json:"version"`
	SessionID         string              `json:"sessionId"`
	StorageGeneration string              `json:"storageGeneration"`
	DurableSequence   uint64              `json:"durableSequence"`
	TotalTurns        int                 `json:"totalTurns"`
	Entries           []PersistentMessage `json:"entries"`
	Title             string              `json:"title,omitempty"`
	ModelRef          string              `json:"modelRef,omitempty"`
	ModelIdentity     string              `json:"modelIdentity,omitempty"`
}

type storageIdentity struct {
	Version         int       `json:"version"`
	SessionID       string    `json:"sessionId"`
	Generation      string    `json:"generation"`
	LogPrefixDigest string    `json:"logPrefixDigest,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type recoveryCheckpoint struct {
	Version           int                `json:"version"`
	SessionID         string             `json:"sessionId"`
	StorageGeneration string             `json:"storageGeneration"`
	StorageRevision   int                `json:"storageRevision"`
	DurableSequence   uint64             `json:"durableSequence"`
	LogOffset         int64              `json:"logOffset"`
	AnchorOffset      int64              `json:"anchorOffset,omitempty"`
	AnchorFirst       uint64             `json:"anchorFirst,omitempty"`
	AnchorCommitID    string             `json:"anchorCommitId,omitempty"`
	AnchorHash        string             `json:"anchorHash,omitempty"`
	ProjectionVersion int                `json:"projectionVersion"`
	Projection        Projection         `json:"projection"`
	RecentMessages    []provider.Message `json:"recentMessages,omitempty"`
	CatalogPreview    string             `json:"catalogPreview,omitempty"`
	CreatedAt         time.Time          `json:"createdAt"`
}

type recoveryOperation struct {
	Hash             string    `json:"hash"`
	CommitID         string    `json:"commitId"`
	FirstSequence    uint64    `json:"firstSequence"`
	EventCount       int       `json:"eventCount"`
	TurnID           string    `json:"turnId,omitempty"`
	OperationID      string    `json:"operationId"`
	OperationHash    string    `json:"operationHash"`
	WriterGeneration uint64    `json:"writerGeneration"`
	CreatedAt        time.Time `json:"createdAt"`
}

func operationForRecovery(record operationRecord) recoveryOperation {
	commit := record.commit
	return recoveryOperation{
		Hash: record.hash, CommitID: commit.ID, FirstSequence: commit.FirstSequence,
		EventCount: commit.EventCount, TurnID: commit.TurnID, OperationID: commit.OperationID,
		OperationHash: commit.OperationHash, WriterGeneration: commit.WriterGeneration,
		CreatedAt: commit.CreatedAt,
	}
}

func (o recoveryOperation) record() operationRecord {
	return operationRecord{hash: o.Hash, commit: Commit{
		SchemaVersion: SchemaVersion, Codec: Codec, RecordType: "commit", ID: o.CommitID,
		OperationID: o.OperationID, OperationHash: o.OperationHash,
		FirstSequence: o.FirstSequence, EventCount: o.EventCount, TurnID: o.TurnID,
		WriterGeneration: o.WriterGeneration, CreatedAt: o.CreatedAt,
	}}
}

type recoveryStore struct {
	db         *bolt.DB
	path       string
	recent     string
	sessionDir string
	identity   storageIdentity
}

func recoveryCacheDir(sessionDir string) string {
	return filepath.Join(filepath.Dir(sessionDir), ".recovery-cache", filepath.Base(sessionDir))
}

func ensureStorageIdentity(sessionDir string, manifest Manifest) (storageIdentity, error) {
	path := filepath.Join(sessionDir, storageIdentityName)
	prefix, prefixErr := storageLogPrefix(sessionDir, manifest)
	if prefixErr != nil && !os.IsNotExist(prefixErr) {
		return storageIdentity{}, prefixErr
	}
	if data, err := os.ReadFile(path); err == nil {
		var identity storageIdentity
		if json.Unmarshal(data, &identity) == nil && identity.Version == recoveryFormatVersion && identity.SessionID == manifest.SessionID && strings.TrimSpace(identity.Generation) != "" {
			if identity.LogPrefixDigest == "" && prefix != "" {
				identity.LogPrefixDigest = prefix
				encoded, marshalErr := json.Marshal(identity)
				if marshalErr != nil {
					return storageIdentity{}, marshalErr
				}
				if writeErr := fileutil.AtomicWriteFileStrict(path, append(encoded, '\n'), 0o600); writeErr != nil {
					return storageIdentity{}, writeErr
				}
				return identity, nil
			}
			if prefix == "" || identity.LogPrefixDigest == prefix {
				return identity, nil
			}
		}
	}
	identity := storageIdentity{Version: recoveryFormatVersion, SessionID: manifest.SessionID, Generation: randomID(), LogPrefixDigest: prefix, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(identity)
	if err != nil {
		return storageIdentity{}, err
	}
	if err := fileutil.AtomicWriteFileStrict(path, append(data, '\n'), 0o600); err != nil {
		return storageIdentity{}, err
	}
	return identity, nil
}

func storageLogPrefix(sessionDir string, manifest Manifest) (string, error) {
	file, err := os.Open(logPathForManifest(sessionDir, manifest))
	if err != nil {
		return "", err
	}
	defer file.Close()
	// The framed transaction header makes the first 64 bytes immutable after
	// the first commit. Hashing a larger short-file prefix would change merely
	// because a normal append extended a log shorter than that prefix.
	buffer := make([]byte, 64)
	n, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	digest := sha256.Sum256(buffer[:n])
	return fmt.Sprintf("%x", digest[:]), nil
}

func readStorageIdentity(sessionDir string, manifest Manifest) (storageIdentity, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, storageIdentityName))
	if err != nil {
		return storageIdentity{}, err
	}
	var identity storageIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return storageIdentity{}, err
	}
	if identity.Version != recoveryFormatVersion || identity.SessionID != manifest.SessionID || strings.TrimSpace(identity.Generation) == "" {
		return storageIdentity{}, ErrStaleGeneration
	}
	prefix, err := storageLogPrefix(sessionDir, manifest)
	if err != nil && !os.IsNotExist(err) {
		return storageIdentity{}, err
	}
	if identity.LogPrefixDigest != "" && prefix != identity.LogPrefixDigest {
		return storageIdentity{}, ErrStaleGeneration
	}
	return identity, nil
}

func openRecoveryStore(sessionDir string, identity storageIdentity) (*recoveryStore, error) {
	dir := recoveryCacheDir(sessionDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, recoveryDBName)
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 250 * time.Millisecond, NoFreelistSync: true})
	if err != nil {
		return nil, err
	}
	store := &recoveryStore{db: db, path: path, recent: filepath.Join(dir, recentSnapshotName), sessionDir: sessionDir, identity: identity}
	err = db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(recoveryMetaBucket)
		if err != nil {
			return err
		}
		checkpoints, err := tx.CreateBucketIfNotExists(recoveryCheckpointBucket)
		if err != nil {
			return err
		}
		operations, err := tx.CreateBucketIfNotExists(recoveryOperationBucket)
		if err != nil {
			return err
		}
		_ = checkpoints
		_ = operations
		stored := string(meta.Get([]byte("storage_generation")))
		if stored != "" && stored != identity.Generation {
			if err := tx.DeleteBucket(recoveryCheckpointBucket); err != nil {
				return err
			}
			if err := tx.DeleteBucket(recoveryOperationBucket); err != nil {
				return err
			}
			if _, err := tx.CreateBucket(recoveryCheckpointBucket); err != nil {
				return err
			}
			if _, err := tx.CreateBucket(recoveryOperationBucket); err != nil {
				return err
			}
		}
		return meta.Put([]byte("storage_generation"), []byte(identity.Generation))
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func openRecoveryStoreRepair(sessionDir string, identity storageIdentity) (*recoveryStore, error) {
	store, err := openRecoveryStore(sessionDir, identity)
	if err == nil {
		return store, nil
	}
	path := filepath.Join(recoveryCacheDir(sessionDir), recoveryDBName)
	if _, statErr := os.Stat(path); statErr == nil {
		_ = os.Rename(path, path+fmt.Sprintf(".corrupt-%d", time.Now().UTC().UnixNano()))
	}
	return openRecoveryStore(sessionDir, identity)
}

func (s *recoveryStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func encodeRecoveryValue(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, err
	}
	defer encoder.Close()
	return encoder.EncodeAll(raw, nil), nil
}

func decodeRecoveryValue(data []byte, value any) error {
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20))
	if err != nil {
		return err
	}
	defer decoder.Close()
	raw, err := decoder.DecodeAll(data, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

func (s *recoveryStore) loadCheckpoints() ([]recoveryCheckpoint, error) {
	if s == nil || s.db == nil {
		return nil, os.ErrNotExist
	}
	var encoded [][]byte
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(recoveryCheckpointBucket)
		if bucket == nil {
			return os.ErrNotExist
		}
		for _, key := range [][]byte{recoveryCurrentKey, recoveryPreviousKey} {
			if value := bucket.Get(key); value != nil {
				encoded = append(encoded, append([]byte(nil), value...))
			}
		}
		if len(encoded) == 0 {
			return os.ErrNotExist
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	checkpoints := make([]recoveryCheckpoint, 0, len(encoded))
	for _, value := range encoded {
		var checkpoint recoveryCheckpoint
		if decodeRecoveryValue(value, &checkpoint) == nil {
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	if len(checkpoints) == 0 {
		return nil, ErrDamagedStore
	}
	return checkpoints, nil
}

func (s *recoveryStore) lookupOperation(operationID string) (operationRecord, bool, error) {
	if s == nil || s.db == nil {
		return operationRecord{}, false, nil
	}
	var encoded []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(recoveryOperationBucket)
		if bucket == nil {
			return nil
		}
		encoded = append(encoded, bucket.Get([]byte(operationID))...)
		return nil
	})
	if err != nil || len(encoded) == 0 {
		return operationRecord{}, false, err
	}
	var operation recoveryOperation
	if err := json.Unmarshal(encoded, &operation); err != nil {
		return operationRecord{}, false, err
	}
	return operation.record(), true, nil
}

func (s *recoveryStore) publish(ctx context.Context, checkpoint recoveryCheckpoint, operations map[string]operationRecord) error {
	if s == nil || s.db == nil {
		return errors.New("session: recovery store unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.identity.LogPrefixDigest == "" {
		manifest, err := readStoredManifest(filepath.Join(s.sessionDir, "manifest.json"))
		if err != nil {
			return err
		}
		prefix, err := storageLogPrefix(s.sessionDir, manifest)
		if err != nil {
			return err
		}
		if prefix != "" {
			s.identity.LogPrefixDigest = prefix
			encodedIdentity, err := json.Marshal(s.identity)
			if err != nil {
				return err
			}
			if err := fileutil.AtomicWriteFileStrict(filepath.Join(s.sessionDir, storageIdentityName), append(encodedIdentity, '\n'), 0o600); err != nil {
				return err
			}
		}
	}
	checkpoint.Version = recoveryFormatVersion
	checkpoint.StorageGeneration = s.identity.Generation
	checkpoint.CreatedAt = time.Now().UTC()
	encoded, err := encodeRecoveryValue(checkpoint)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(operations))
	for key := range operations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	err = s.db.Update(func(tx *bolt.Tx) error {
		checkpoints := tx.Bucket(recoveryCheckpointBucket)
		operationBucket := tx.Bucket(recoveryOperationBucket)
		if checkpoints == nil || operationBucket == nil {
			return errors.New("session: recovery buckets unavailable")
		}
		if current := checkpoints.Get(recoveryCurrentKey); current != nil {
			if err := checkpoints.Put(recoveryPreviousKey, current); err != nil {
				return err
			}
		}
		for _, key := range keys {
			operation := operationForRecovery(operations[key])
			value, err := json.Marshal(operation)
			if err != nil {
				return err
			}
			if err := operationBucket.Put([]byte(key), value); err != nil {
				return err
			}
		}
		if err := checkpoints.Put(recoveryCurrentKey, encoded); err != nil {
			return err
		}
		return tx.Bucket(recoveryMetaBucket).Put([]byte("coverage_sequence"), fmt.Append(nil, checkpoint.DurableSequence))
	})
	if err != nil {
		return err
	}
	recent := RecentSnapshot{
		Version: recoveryFormatVersion, SessionID: checkpoint.SessionID,
		StorageGeneration: checkpoint.StorageGeneration, DurableSequence: checkpoint.DurableSequence,
		Title:    checkpoint.Projection.Title,
		ModelRef: checkpoint.Projection.ModelRef, ModelIdentity: checkpoint.Projection.ModelIdentity,
		TotalTurns: visibleBoundaryCount(checkpoint.Projection, false),
	}
	recent.Entries, err = buildRecentEntries(ctx, s.sessionDir, checkpoint.RecentMessages, checkpoint.DurableSequence, recent.TotalTurns)
	attachSubmissionEntries(checkpoint.Projection.Submissions, checkpoint.SessionID, recent.Entries)
	if err != nil {
		return err
	}
	data, err := json.Marshal(recent)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(s.recent, append(data, '\n'), 0o600)
}

func readRecentSnapshot(sessionDir string, identity storageIdentity) (RecentSnapshot, error) {
	data, err := os.ReadFile(filepath.Join(recoveryCacheDir(sessionDir), recentSnapshotName))
	if err != nil {
		return RecentSnapshot{}, err
	}
	var snapshot RecentSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return RecentSnapshot{}, err
	}
	if snapshot.Version != recoveryFormatVersion || snapshot.SessionID != identity.SessionID || snapshot.StorageGeneration != identity.Generation {
		return RecentSnapshot{}, ErrStaleGeneration
	}
	if len(snapshot.Entries) > RecentMessageLimit {
		return RecentSnapshot{}, ErrDamagedStore
	}
	return snapshot, nil
}

// buildRecentEntries produces the bounded public baseline. Large canonical
// messages are stored once in ContentStore and represented by a preview plus a
// range-readable reference, so recent-v1.json cannot grow with tool output.
func buildRecentEntries(ctx context.Context, sessionDir string, messages []provider.Message, sequence uint64, totalTurns int) ([]PersistentMessage, error) {
	if len(messages) > RecentMessageLimit {
		messages = messages[len(messages)-RecentMessageLimit:]
	}
	entries := make([]PersistentMessage, 0, len(messages))
	content := contentStoreForSessionDir(sessionDir)
	inlineBytes := 0
	visibleTurn := totalTurns
	for _, message := range messages {
		if agent.IsUserAuthoredTurnMessage(message) {
			visibleTurn--
		}
	}
	visibleTurn = max(visibleTurn, 0)
	for position, message := range messages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if agent.IsUserAuthoredTurnMessage(message) {
			visibleTurn++
		}
		body, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		entry := PersistentMessage{
			MessageID: message.ID, Position: int64(position + 1), Version: 1,
			Role: string(message.Role), Preview: messagePreview(message),
			EventSequence: sequence, VisibleTurn: visibleTurn,
		}
		if len(body) <= recentInlineBytes && inlineBytes+len(body) <= recentResponseBytes {
			entry.Inline = body
			inlineBytes += len(body)
		} else {
			ref, err := content.Put(ctx, bytes.NewReader(body), sessioncontent.Metadata{MediaType: "application/json"})
			if err != nil {
				return nil, err
			}
			entry.ContentRef = &ref
			previewBody, err := recentDisplayMessage(message)
			if err != nil {
				return nil, err
			}
			if inlineBytes+len(previewBody) <= recentResponseBytes {
				entry.Inline = previewBody
				inlineBytes += len(previewBody)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func recentDisplayMessage(message provider.Message) (json.RawMessage, error) {
	preview := detachMessages([]provider.Message{message})[0]
	preview.Content = messagePreview(message)
	preview.RawContent = ""
	preview.ProviderContent = ""
	preview.Images = nil
	preview.ImageInputs = nil
	preview.ResponsesItems = nil
	preview.ThinkingBlocks = nil
	if runes := []rune(preview.ReasoningContent); len(runes) > 4096 {
		preview.ReasoningContent = string(runes[:4096])
	}
	for i := range preview.ToolCalls {
		if runes := []rune(preview.ToolCalls[i].Arguments); len(runes) > 2048 {
			preview.ToolCalls[i].Arguments = string(runes[:2048])
		}
	}
	body, err := json.Marshal(preview)
	if err != nil {
		return nil, err
	}
	if len(body) <= recentInlineBytes {
		return body, nil
	}
	// Preserve the fields required to place the row even when optional display
	// metadata alone exceeds the per-message preview budget.
	return json.Marshal(provider.Message{
		ID: preview.ID, Role: preview.Role, Content: preview.Content,
		ToolCallID: preview.ToolCallID, Name: preview.Name,
		CreatedAt: preview.CreatedAt, WorkDurationMs: preview.WorkDurationMs,
	})
}

func checkpointFromStartup(manifest Manifest, identity storageIdentity, state *startupSessionState) recoveryCheckpoint {
	projection, _ := Project(nil)
	if state != nil {
		projection = cloneProjection(state.projection)
		projection.Messages = nil
	}
	checkpoint := recoveryCheckpoint{
		Version: recoveryFormatVersion, SessionID: manifest.SessionID,
		StorageGeneration: identity.Generation, StorageRevision: StorageRevision,
		ProjectionVersion: recoveryProjectionVersion, Projection: projection,
	}
	if state != nil {
		checkpoint.DurableSequence = state.durable
		checkpoint.LogOffset = state.tip.LogOffset
		checkpoint.AnchorOffset = state.tip.AnchorOffset
		checkpoint.AnchorFirst = state.tip.AnchorFirst
		checkpoint.AnchorCommitID = state.tip.AnchorCommitID
		checkpoint.AnchorHash = state.tip.AnchorHash
		checkpoint.RecentMessages = detachMessages(state.recentMessages)
		checkpoint.CatalogPreview = state.catalogPreview
	}
	return checkpoint
}

func loadRecoveryStartupState(ctx context.Context, dir string, file *os.File, info os.FileInfo, recovery *recoveryStore, identity storageIdentity) (*startupSessionState, int64, bool, RecoveryOpenStats, bool) {
	stats := RecoveryOpenStats{LogBytesTotal: info.Size()}
	checkpoints, err := recovery.loadCheckpoints()
	if err != nil {
		return nil, 0, false, stats, false
	}
	for _, checkpoint := range checkpoints {
		if state, end, torn, attempt, ok := tryRecoveryCheckpoint(ctx, dir, file, info, identity, checkpoint); ok {
			return state, end, torn, attempt, true
		}
	}
	return nil, 0, false, stats, false
}

func tryRecoveryCheckpoint(ctx context.Context, dir string, file *os.File, info os.FileInfo, identity storageIdentity, checkpoint recoveryCheckpoint) (*startupSessionState, int64, bool, RecoveryOpenStats, bool) {
	stats := RecoveryOpenStats{LogBytesTotal: info.Size()}
	if checkpoint.Version != recoveryFormatVersion || checkpoint.ProjectionVersion != recoveryProjectionVersion ||
		checkpoint.SessionID != identity.SessionID || checkpoint.StorageGeneration != identity.Generation ||
		checkpoint.StorageRevision != StorageRevision || checkpoint.LogOffset < 0 || checkpoint.LogOffset > info.Size() ||
		checkpoint.Projection.CommittedSequence != checkpoint.DurableSequence {
		return nil, 0, false, stats, false
	}
	if checkpoint.DurableSequence == 0 {
		if checkpoint.LogOffset != 0 {
			return nil, 0, false, stats, false
		}
	} else {
		if checkpoint.AnchorOffset < 0 || checkpoint.AnchorOffset >= checkpoint.LogOffset || checkpoint.AnchorFirst == 0 || checkpoint.AnchorCommitID == "" {
			return nil, 0, false, stats, false
		}
		var anchor Commit
		var anchorEnd int64
		err := scanV4CommitFileRefs(ctx, file, checkpoint.AnchorOffset, checkpoint.AnchorFirst, contentStoreForSessionDir(dir), nil, func(_ int64, commit Commit) bool {
			anchor = commit
			anchorEnd, _ = file.Seek(0, 1)
			return false
		})
		if err != nil || anchor.ID != checkpoint.AnchorCommitID || anchor.OperationHash != checkpoint.AnchorHash ||
			anchor.LastSequence() != checkpoint.DurableSequence || anchorEnd != checkpoint.LogOffset {
			return nil, 0, false, stats, false
		}
		stats.LogBytesRead += anchorEnd - checkpoint.AnchorOffset
	}

	state := &startupSessionState{
		projection: cloneProjection(checkpoint.Projection), operations: map[string]operationRecord{},
		durable: checkpoint.DurableSequence, catalogPreview: checkpoint.CatalogPreview,
		recentMessages: detachMessages(checkpoint.RecentMessages),
		tip: durableTip{LogOffset: checkpoint.LogOffset, AnchorOffset: checkpoint.AnchorOffset,
			AnchorFirst: checkpoint.AnchorFirst, AnchorCommitID: checkpoint.AnchorCommitID, AnchorHash: checkpoint.AnchorHash},
	}
	var projectionErr error
	content := contentStoreForSessionDir(dir)
	err := scanV4CommitFile(ctx, file, checkpoint.LogOffset, checkpoint.DurableSequence+1, content, nil, func(offset int64, commit Commit) bool {
		if err := applyProjectionCommit(&state.projection, commit); err != nil {
			projectionErr = err
			return false
		}
		if err := applyRecentCommit(&state.recentMessages, commit); err != nil {
			projectionErr = err
			return false
		}
		state.operations[commit.OperationID] = compactOperationRecord(commit)
		state.durable = commit.LastSequence()
		state.tip.AnchorOffset = offset
		state.tip.AnchorFirst = commit.FirstSequence
		state.tip.AnchorCommitID = commit.ID
		state.tip.AnchorHash = commit.OperationHash
		state.tip.LogOffset, _ = file.Seek(0, 1)
		stats.TailCommits++
		return true
	})
	if err != nil || projectionErr != nil {
		return nil, 0, false, stats, false
	}
	state.projection.Messages = nil
	state.projection.CommittedSequence = state.durable
	stats.UsedCheckpoint = true
	stats.LogBytesRead += max(state.tip.LogOffset-checkpoint.LogOffset, 0)
	return state, state.tip.LogOffset, state.tip.LogOffset < info.Size(), stats, true
}

func applyRecentCommit(messages *[]provider.Message, commit Commit) error {
	projection, _ := Project(nil)
	projection.Messages = detachMessages(*messages)
	recent := commit
	recent.Events = nil
	for _, event := range commit.Events {
		switch event.Kind {
		case "message/complete", "message/upsert", "message/retract", "history/replace", "legacy/import":
			recent.Events = append(recent.Events, event)
		}
	}
	if len(recent.Events) == 0 {
		return nil
	}
	if err := applyProjectionCommit(&projection, recent); err != nil {
		return err
	}
	if len(projection.Messages) > RecentMessageLimit {
		projection.Messages = append([]provider.Message(nil), projection.Messages[len(projection.Messages)-RecentMessageLimit:]...)
	}
	*messages = detachMessages(projection.Messages)
	return nil
}
