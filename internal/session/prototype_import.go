package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/fileutil"
	filelock "reasonix/internal/identitylock"
)

type frozenPreview struct {
	dir           string
	freezeDir     string
	manifestBytes []byte
	eventPath     string
	manifest      Manifest
	source        Source
	logName       string
}

type PrototypeImportResult struct {
	TargetID       string
	TargetDir      string
	Source         Source
	Reused         bool
	ImportedEvents uint64
}

// ImportPrototype performs the deliberately narrow bridge from the sidecar
// prototype codec to the final linear codec. It accepts only the complete,
// validated event prefix. Unknown required events and damaged complete records
// fail closed; an unterminated tail is preserved with the frozen source.
func ImportPrototype(ctx context.Context, sourceDir, targetRoot string) (PrototypeImportResult, error) {
	return importPreview(ctx, sourceDir, targetRoot)
}

// ImportStoredPreview uses the existing explicit adapter for pre-ownership
// stores, but publishes to a separate staging root. The source is never
// upgraded in place, including for the unpublished v4 draft.
func ImportStoredPreview(ctx context.Context, sourceDir, targetRoot string) (PrototypeImportResult, error) {
	if err := ctx.Err(); err != nil {
		return PrototypeImportResult{}, err
	}
	if strings.TrimSpace(targetRoot) == "" || filepath.Clean(targetRoot) == "." {
		return PrototypeImportResult{}, errors.New("session: preview target root is required")
	}
	frozen, err := freezePairedPreview(ctx, sourceDir)
	if err != nil {
		return PrototypeImportResult{}, err
	}
	defer os.RemoveAll(frozen.freezeDir)
	return importFrozenPreview(ctx, frozen, targetRoot, CreateOptions{})
}

// importPreview accepts both retired prototype codecs produced before the
// identity cutover. Callers must resolve it together with the paired legacy
// transcript; opening either source in isolation can silently drop newer work.
func importPreview(ctx context.Context, sourceDir, targetRoot string) (PrototypeImportResult, error) {
	if err := ctx.Err(); err != nil {
		return PrototypeImportResult{}, err
	}
	sourceDir = filepath.Clean(strings.TrimSpace(sourceDir))
	targetRoot = filepath.Clean(strings.TrimSpace(targetRoot))
	if sourceDir == "." || targetRoot == "." {
		return PrototypeImportResult{}, fmt.Errorf("session: prototype source and target root are required")
	}
	frozen, err := freezePreview(ctx, sourceDir)
	if err != nil {
		return PrototypeImportResult{}, err
	}
	defer os.RemoveAll(frozen.freezeDir)
	return importFrozenPreview(ctx, frozen, targetRoot, CreateOptions{})
}

func freezePreview(ctx context.Context, sourceDir string) (frozenPreview, error) {
	return freezePreviewCodec(ctx, sourceDir, false)
}

// freezePairedPreview also accepts the unpublished v4 draft (missing
// storageRevision). Only migration may interpret that layout; normal session
// opens require the final revision explicitly.
func freezePairedPreview(ctx context.Context, sourceDir string) (frozenPreview, error) {
	return freezePreviewCodec(ctx, sourceDir, true)
}

func freezePreviewCodec(ctx context.Context, sourceDir string, allowCurrent bool) (frozenPreview, error) {
	if _, err := os.Stat(sourceDir); err != nil {
		// Report absence before taking any lock. The ownership lock lives beside
		// the directory, so a missing candidate must not surface as a lock error
		// that callers cannot classify as "no paired source".
		return frozenPreview{}, err
	}
	// Prepare never waits behind a live writer. After the host suspends its own
	// producer, any remaining owner makes this import ineligible.
	releaseDirectory, err := filelock.TryAcquireMode(directoryOwnershipPath(sourceDir), filelock.ModeShared)
	if err != nil {
		if errors.Is(err, filelock.ErrHeld) {
			return frozenPreview{}, fmt.Errorf("%w: freeze preview ownership", ErrWriterOwned)
		}
		return frozenPreview{}, fmt.Errorf("freeze preview ownership: %w", err)
	}
	defer releaseDirectory()
	info, err := os.Stat(sourceDir)
	if err != nil {
		return frozenPreview{}, err
	}
	if !info.IsDir() {
		return frozenPreview{}, fmt.Errorf("session: preview path is not a directory: %s", sourceDir)
	}
	releaseWriter, err := filelock.TryAcquireMode(filepath.Join(sourceDir, "writer.lock"), filelock.ModeShared)
	if err != nil {
		if errors.Is(err, filelock.ErrHeld) {
			return frozenPreview{}, fmt.Errorf("%w: freeze preview writer", ErrWriterOwned)
		}
		return frozenPreview{}, fmt.Errorf("freeze preview writer: %w", err)
	}
	defer releaseWriter()
	manifestBytes, err := os.ReadFile(filepath.Join(sourceDir, "manifest.json"))
	if err != nil {
		return frozenPreview{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return frozenPreview{}, fmt.Errorf("%w: prototype manifest: %w", ErrDamagedStore, err)
	}
	legacyCodec := manifest.SchemaVersion == 3 && (manifest.Codec == PrototypeCodec || manifest.Codec == LegacyLinearCodec || manifest.Codec == FinalV31Codec)
	draftCodec := allowCurrent && manifest.SchemaVersion == SchemaVersion && manifest.Codec == Codec && manifest.StorageRevision == 0
	if (!legacyCodec && !draftCodec) || strings.TrimSpace(manifest.SessionID) == "" {
		return frozenPreview{}, fmt.Errorf("%w: unsupported preview codec %q", ErrUnsupportedVersion, manifest.Codec)
	}
	logName := legacyLogName
	if draftCodec {
		logName = currentLogName
	}
	freezeDir, err := os.MkdirTemp("", "reasonix-preview-freeze-*")
	if err != nil {
		return frozenPreview{}, err
	}
	keepFreeze := false
	defer func() {
		if !keepFreeze {
			_ = os.RemoveAll(freezeDir)
		}
	}()
	frozenManifestPath := filepath.Join(freezeDir, "manifest.json")
	if err := fileutil.AtomicWriteFileStrict(frozenManifestPath, manifestBytes, 0o600); err != nil {
		return frozenPreview{}, err
	}
	frozenEventPath := filepath.Join(freezeDir, logName)
	sourceEventPath := filepath.Join(sourceDir, logName)
	if err := copyFrozenArtifact(ctx, sourceEventPath, frozenEventPath, 0o600); os.IsNotExist(err) {
		if err := fileutil.AtomicWriteFileStrict(frozenEventPath, nil, 0o600); err != nil {
			return frozenPreview{}, err
		}
	} else if err != nil {
		return frozenPreview{}, err
	}
	digest := sha256.New()
	digest.Write(manifestBytes)
	digest.Write([]byte{0})
	eventFile, err := os.Open(frozenEventPath)
	if err != nil {
		return frozenPreview{}, err
	}
	eventSize, err := copyStreamWithContext(ctx, digest, eventFile)
	closeErr := eventFile.Close()
	if err != nil {
		return frozenPreview{}, err
	}
	if closeErr != nil {
		return frozenPreview{}, closeErr
	}
	sourceDigest := hex.EncodeToString(digest.Sum(nil))
	source := Source{Path: sourceDir, Size: int64(len(manifestBytes)) + eventSize, SHA256: sourceDigest, Version: manifest.Codec}
	keepFreeze = true
	return frozenPreview{dir: sourceDir, freezeDir: freezeDir, manifestBytes: manifestBytes, eventPath: frozenEventPath, manifest: manifest, source: source, logName: logName}, nil
}

func importFrozenPreview(ctx context.Context, frozen frozenPreview, targetRoot string, options CreateOptions) (PrototypeImportResult, error) {
	prototype, source := frozen.manifest, frozen.source
	manifestBytes, sourceDir := frozen.manifestBytes, frozen.dir
	targetID := deterministicID("prototype-import\x00" + prototype.Codec + "\x00" + sourceDir + "\x00" + source.SHA256)
	targetDir := filepath.Join(targetRoot, targetID)
	result := PrototypeImportResult{TargetID: targetID, TargetDir: targetDir, Source: source}
	reused, inherited, err := reuseFrozenPreviewTarget(targetDir, targetID, source, prototype.Codec, options)
	if err != nil {
		return PrototypeImportResult{}, err
	}
	if reused {
		result.Reused, result.ImportedEvents = true, inherited
		return result, nil
	}
	if err := os.MkdirAll(targetRoot, 0o700); err != nil {
		return PrototypeImportResult{}, err
	}
	tmp, err := os.MkdirTemp(targetRoot, "."+targetID+".prototype-")
	if err != nil {
		return PrototypeImportResult{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(tmp)
		}
	}()
	legacyDir := filepath.Join(tmp, "legacy", "prototype")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		return PrototypeImportResult{}, err
	}
	if err := fileutil.AtomicWriteFileStrict(filepath.Join(legacyDir, "manifest.json"), manifestBytes, 0o600); err != nil {
		return PrototypeImportResult{}, err
	}
	if err := copyFrozenArtifact(ctx, frozen.eventPath, filepath.Join(legacyDir, frozen.logName), 0o600); err != nil {
		return PrototypeImportResult{}, err
	}

	frozenLog, err := os.Open(frozen.eventPath)
	if err != nil {
		return PrototypeImportResult{}, err
	}
	finalLogPath := filepath.Join(tmp, currentLogName)
	finalLog, err := os.OpenFile(finalLogPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = frozenLog.Close()
		return PrototypeImportResult{}, err
	}
	content := contentStoreForSessionDir(tmp)
	projection := Projection{}
	var importedCommits int
	var lastSequence uint64
	var lastActivity time.Time
	var convertErr error
	pending := make([]Commit, 0, 64)
	flushPending := func() error {
		if len(pending) == 0 {
			return nil
		}
		_, err := encodeV4Commits(ctx, finalLog, content, pending)
		pending = pending[:0]
		return err
	}
	knownKinds := ProjectionKinds
	if prototype.Codec == PrototypeCodec {
		knownKinds = PrototypeProjectionKinds
	}
	visit := func(_ int64, commit Commit) bool {
		converted, err := convertPrototypeCommit(commit, prototype.SessionID, targetID, prototype.Codec)
		if err == nil {
			err = applyProjectionCommit(&projection, converted)
		}
		if err == nil {
			pending = append(pending, converted)
			if len(pending) == cap(pending) {
				err = flushPending()
			}
		}
		if err != nil {
			convertErr = err
			return false
		}
		importedCommits++
		lastSequence = converted.LastSequence()
		if converted.CreatedAt.After(lastActivity) {
			lastActivity = converted.CreatedAt
		}
		return ctx.Err() == nil
	}
	if prototype.Codec == Codec && prototype.StorageRevision == 0 {
		err = scanV4CommitFile(ctx, frozenLog, 0, 1, contentStoreForSessionDir(sourceDir), knownKinds, visit)
	} else {
		err = scanCommitFileCodec(frozenLog, 0, 1, prototype.Codec, knownKinds, visit)
	}
	frozenCloseErr := frozenLog.Close()
	if err != nil {
		_ = finalLog.Close()
		return PrototypeImportResult{}, err
	}
	if convertErr != nil {
		_ = finalLog.Close()
		return PrototypeImportResult{}, convertErr
	}
	if err := flushPending(); err != nil {
		_ = finalLog.Close()
		return PrototypeImportResult{}, err
	}
	if err := ctx.Err(); err != nil {
		_ = finalLog.Close()
		return PrototypeImportResult{}, err
	}
	if frozenCloseErr != nil {
		_ = finalLog.Close()
		return PrototypeImportResult{}, frozenCloseErr
	}
	if err := finalLog.Sync(); err != nil {
		_ = finalLog.Close()
		return PrototypeImportResult{}, err
	}
	if err := finalLog.Close(); err != nil {
		return PrototypeImportResult{}, err
	}
	createdAt, updatedAt := prototypeSourceTimes(&source, prototype, lastActivity)
	finalManifest := Manifest{
		SchemaVersion: SchemaVersion, Codec: Codec, StorageRevision: StorageRevision, ContentRoot: sharedContentRoot, SessionID: targetID,
		CreatedAt: createdAt, InheritedEvents: lastSequence, Source: &source,
	}
	if err := writeImportedPreviewManifest(tmp, finalManifest, options); err != nil {
		return PrototypeImportResult{}, err
	}
	if err := validateImportedPreview(ctx, finalLogPath, content, importedCommits, lastSequence); err != nil {
		return PrototypeImportResult{}, err
	}
	if err := stampLogActivity(finalLogPath, updatedAt); err != nil {
		return PrototypeImportResult{}, err
	}
	if err := os.Rename(tmp, targetDir); err != nil {
		return PrototypeImportResult{}, fmt.Errorf("publish prototype target: %w", err)
	}
	published = true
	result.ImportedEvents = lastSequence
	return result, nil
}

func reuseFrozenPreviewTarget(targetDir, targetID string, source Source, codec string, options CreateOptions) (bool, uint64, error) {
	manifest, err := readManifest(filepath.Join(targetDir, "manifest.json"))
	if os.IsNotExist(err) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if manifest.Source == nil || manifest.Source.Path != source.Path || manifest.Source.SHA256 != source.SHA256 || manifest.Source.Version != codec {
		return false, 0, fmt.Errorf("%w: prototype target %s has another source", ErrSessionExists, targetID)
	}
	if err := validateSessionHeaderForCreate(targetDir, targetID, options); err != nil {
		return false, 0, err
	}
	return true, manifest.InheritedEvents, nil
}

func writeImportedPreviewManifest(dir string, manifest Manifest, options CreateOptions) error {
	if err := writeManifestFile(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		return err
	}
	return writeSessionHeaderForCreate(dir, manifest.SessionID, manifest.CreatedAt, options)
}

func convertPrototypeCommit(original Commit, sourceID, targetID, sourceCodec string) (Commit, error) {
	commit := cloneCommit(original)
	commit.SchemaVersion = SchemaVersion
	commit.Codec = Codec
	commit.ID = deterministicID("prototype-commit\x00" + targetID + "\x00" + original.ID)
	commit.OperationID = "prototype:" + sourceID + ":" + original.ID
	commit.WriterGeneration = 1
	for eventIndex := range commit.Events {
		if sourceCodec == PrototypeCodec && commit.Events[eventIndex].Kind == "context/replace" {
			commit.Events[eventIndex].Kind = "history/replace"
		}
	}
	operationHash, err := hashOperation(targetID, commit.TurnID, commit.Events)
	if err != nil {
		return Commit{}, err
	}
	commit.OperationHash = operationHash
	return commit, nil
}
