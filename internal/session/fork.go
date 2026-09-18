package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/fileutil"
)

// Fork publishes an independent child session containing the exact durable
// prefix through a completed turn. The cut must also be a logical batch
// boundary, so a child can never inherit half of an atomic operation.
//
// Fork is a Session operation because the inherited prefix is business state:
// the physical layer only writes the child bytes.
//
// A read-only session is a legitimate source. The child is built from the
// parent's durable prefix and no byte is ever written back, so forking a
// session that another process owns, or one read cold from disk, must not
// require the parent's writer lease.
func (s *Session) Fork(ctx context.Context, childDir, childID string, throughSequence uint64) (Manifest, error) {
	if s == nil {
		return Manifest{}, fmt.Errorf("session: nil parent session")
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	if s.WritableHandle() != nil {
		if _, err := s.Flush(ctx); err != nil {
			return Manifest{}, fmt.Errorf("flush parent before fork: %w", err)
		}
	} else if !s.cold() {
		return Manifest{}, fmt.Errorf("session: fork needs a durable parent log")
	}
	var prefix []Commit
	var cursor uint64
	for {
		previous := cursor
		page, err := s.Read(ctx, cursor, 1000)
		if err != nil {
			return Manifest{}, err
		}
		stop := false
		for _, commit := range page.Commits {
			if commit.LastSequence() > throughSequence {
				stop = true
				break
			}
			prefix = append(prefix, commit)
		}
		if stop || !page.Truncated {
			break
		}
		if page.Next <= previous {
			return Manifest{}, fmt.Errorf("%w: fork cursor did not advance", ErrDamagedStore)
		}
		cursor = page.Next
	}
	s.mu.Lock()
	parentDir, parentID := s.dirLocked(), s.id
	s.mu.Unlock()
	if throughSequence > 0 && (len(prefix) == 0 || prefix[len(prefix)-1].LastSequence() != throughSequence) {
		return Manifest{}, fmt.Errorf("%w: cut %d", ErrForkBoundaryNotAtomic, throughSequence)
	}
	return writeForkChild(ctx, parentDir, parentID, prefix, childDir, childID, throughSequence)
}

// directoryHandle is implemented by physical handles that know their own
// directory. The leased writer and the cold reader both expose it, so a fork
// can locate the parent's owned files without taking the writer lease.
type directoryHandle interface{ Dir() string }

// cold reports whether this session reads its durable prefix through a cold
// handle rather than a leased writer.
func (s *Session) cold() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.coldHandle != nil
}

// dir reports the physical directory backing this session, if any.
func (s *Session) dir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirLocked()
}

// dirLocked reports the physical directory backing this session, if any. The
// caller holds s.mu.
func (s *Session) dirLocked() string {
	handle := s.coldHandle
	if s.binding != nil {
		handle = s.binding.handle
	}
	if directory, ok := handle.(directoryHandle); ok {
		return directory.Dir()
	}
	return ""
}

// ErrForkActiveAuthority reports a cut whose prefix still carries in-flight
// execution state. The turn ended, but the commit that closed it also opened
// authority a child must not inherit; the caller reports the reason instead of
// trimming the commit.
var ErrForkActiveAuthority = errors.New("session: fork boundary retains active runtime authority")

// ErrForkBoundaryNotAtomic reports a cut that lands inside a logical commit.
// The caller reports the boundary as unverifiable rather than trimming the
// commit to fit.
var ErrForkBoundaryNotAtomic = errors.New("session: fork cut is not an atomic batch boundary")

func forkStorageRevision(prefix []Commit) int {
	for _, commit := range prefix {
		for _, event := range commit.Events {
			if event.Kind == "model/image-isolation" {
				return MaxStorageRevision
			}
		}
	}
	return StorageRevision
}

func writeForkChild(ctx context.Context, parentDir, parentID string, prefix []Commit, childDir, childID string, throughSequence uint64) (Manifest, error) {
	childDir = filepath.Clean(strings.TrimSpace(childDir))
	childID = strings.TrimSpace(childID)
	if childDir == "." || childID == "" {
		return Manifest{}, fmt.Errorf("session: child directory and id are required")
	}
	// filepath.Join("", "attachments") is a relative path, so an empty parent
	// would copy unrelated directories instead of the parent's owned files.
	if strings.TrimSpace(parentDir) == "" {
		return Manifest{}, fmt.Errorf("session: fork requires the parent session directory")
	}
	parentHeader, hasParentHeader, err := readSessionHeader(parentDir, parentID)
	if err != nil {
		return Manifest{}, err
	}
	projection, err := Project(prefix)
	if err != nil {
		return Manifest{}, err
	}
	if forkProjectionAvailability(projection, throughSequence) == ForkActiveAuthority {
		return Manifest{}, ErrForkActiveAuthority
	}

	// Inherited events retain their stable IDs and sequences, while physical
	// commit identity and operation id are rebound to the child. Parent
	// idempotency keys must never suppress a future child submission.
	inherited := make([]Commit, len(prefix))
	for i, original := range prefix {
		commit := cloneCommit(original)
		commit.Codec = Codec
		commit.ID = deterministicID("fork\x00" + childID + "\x00" + original.ID)
		commit.OperationID = "inherit:" + parentID + ":" + original.ID
		commit.WriterGeneration = 1
		hash, hashErr := hashOperation(childID, commit.TurnID, commit.Events)
		if hashErr != nil {
			return Manifest{}, hashErr
		}
		commit.OperationHash = hash
		inherited[i] = commit
	}
	var log bytes.Buffer
	if _, err := encodeV4Commits(ctx, &log, contentStoreForSessionDir(childDir), inherited); err != nil {
		return Manifest{}, err
	}
	digest := sha256.Sum256(log.Bytes())
	manifest := Manifest{
		SchemaVersion: SchemaVersion, Codec: Codec, StorageRevision: forkStorageRevision(prefix), ContentRoot: sharedContentRoot, SessionID: childID, CreatedAt: time.Now().UTC(),
		InheritedEvents: throughSequence,
		Source:          &Source{Path: parentDir, Size: int64(log.Len()), SHA256: hex.EncodeToString(digest[:]), Version: Codec},
	}
	if _, err := os.Stat(childDir); err == nil {
		return Manifest{}, fmt.Errorf("session: child session already exists")
	} else if !os.IsNotExist(err) {
		return Manifest{}, err
	}
	parent := filepath.Dir(childDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return Manifest{}, err
	}
	tmp, err := os.MkdirTemp(parent, "."+childID+".fork-")
	if err != nil {
		return Manifest{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := writeManifestFile(filepath.Join(tmp, "manifest.json"), manifest); err != nil {
		return Manifest{}, err
	}
	if hasParentHeader {
		if err := writeSessionHeader(tmp, SessionHeader{
			SchemaVersion: SessionHeaderSchemaVersion, SessionID: childID, CreatedAt: manifest.CreatedAt,
			CWD: parentHeader.CWD, ParentSessionID: parentID, Origin: SessionOriginFork,
		}); err != nil {
			return Manifest{}, err
		}
	}
	if err := fileutil.AtomicWriteFileStrict(filepath.Join(tmp, currentLogName), log.Bytes(), 0o600); err != nil {
		return Manifest{}, err
	}
	if err := copyOwnedSessionFiles(ctx, parentDir, tmp); err != nil {
		return Manifest{}, fmt.Errorf("copy fork attachments: %w", err)
	}
	if replayed, err := Replay(tmp, nil); err != nil || len(replayed) != len(inherited) {
		if err == nil {
			err = fmt.Errorf("copied %d of %d commits", len(replayed), len(inherited))
		}
		return Manifest{}, fmt.Errorf("validate fork: %w", err)
	}
	if err := os.Rename(tmp, childDir); err != nil {
		return Manifest{}, fmt.Errorf("publish child session: %w", err)
	}
	published = true
	return manifest, nil
}
