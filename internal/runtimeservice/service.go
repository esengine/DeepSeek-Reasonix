// Package runtimeservice contains target-neutral RuntimeAPI business rules.
//
// FileGitService is deliberately unaware of RPC, SSH, Wails, and controller
// layout. Local and Remote callers bind it to one primary workspace and one
// Session checkpoint source, which keeps the RMT-030 file/Git limits identical
// on both targets.
package runtimeservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/runtimeapi"
)

var (
	ErrInvalidSession    = errors.New("runtime service: invalid session target")
	ErrInvalidPath       = errors.New("runtime service: invalid workspace path")
	ErrPathEscapesRoot   = errors.New("runtime service: workspace path escapes primary root")
	ErrPathNotFound      = errors.New("runtime service: workspace path not found")
	ErrNotDirectory      = errors.New("runtime service: workspace path is not a directory")
	ErrNotFile           = errors.New("runtime service: workspace path is not a regular file")
	ErrInvalidCursor     = errors.New("runtime service: invalid cursor")
	ErrStaleCursor       = errors.New("runtime service: stale cursor")
	ErrGitUnavailable    = errors.New("runtime service: Git unavailable")
	ErrGitObjectNotFound = errors.New("runtime service: Git object not found")
	ErrQueryFailed       = errors.New("runtime service: query failed")
)

// CheckpointChange is one primary-relative file observation from a Session
// checkpoint. Multiple observations for the same path are merged by
// WorkspaceChanges.
type CheckpointChange struct {
	Path       string
	Turn       int
	Prompt     string
	TimeMillis int64
}

// CheckpointChangeProvider supplies changes for the Session to which the
// service is bound. Implementations must return a fresh slice or an immutable
// snapshot safe for use after the call returns.
type CheckpointChangeProvider interface {
	CheckpointChanges(context.Context) ([]CheckpointChange, error)
}

type CheckpointChangeProviderFunc func(context.Context) ([]CheckpointChange, error)

func (f CheckpointChangeProviderFunc) CheckpointChanges(ctx context.Context) ([]CheckpointChange, error) {
	return f(ctx)
}

type Options struct {
	// Root is the primary workspace root. It must already exist and be a
	// directory. The constructor resolves it once so a symlinked root has one
	// stable identity for cursor binding and containment checks.
	Root string

	Checkpoints CheckpointChangeProvider

	// GitBinary is primarily an integration seam for packaged builds and
	// unavailable-Git tests. Empty selects "git".
	GitBinary string
}

// FileGitService implements runtimeapi.FileQueryAPI and runtimeapi.GitQueryAPI
// for one primary workspace.
type FileGitService struct {
	root        string
	rootID      string
	checkpoints CheckpointChangeProvider
	gitBinary   string
	cursorKey   [32]byte
}

func NewFileGitService(options Options) (*FileGitService, error) {
	root := strings.TrimSpace(options.Root)
	if root == "" {
		return nil, fmt.Errorf("%w: empty primary root", ErrInvalidPath)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("%w: primary root", ErrInvalidPath)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPathNotFound
		}
		return nil, fmt.Errorf("%w: primary root", ErrQueryFailed)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPathNotFound
		}
		return nil, fmt.Errorf("%w: primary root", ErrQueryFailed)
	}
	if !info.IsDir() {
		return nil, ErrNotDirectory
	}
	resolved = filepath.Clean(resolved)

	service := &FileGitService{
		root:        resolved,
		rootID:      hashText(filepath.Clean(resolved)),
		checkpoints: options.Checkpoints,
		gitBinary:   strings.TrimSpace(options.GitBinary),
	}
	if service.gitBinary == "" {
		service.gitBinary = "git"
	}
	if _, err := rand.Read(service.cursorKey[:]); err != nil {
		return nil, fmt.Errorf("runtime service: initialize cursor key: %w", err)
	}
	return service, nil
}

func (s *FileGitService) Root() string { return s.root }

func requireSession(session runtimeapi.SessionRef) error {
	if !session.Valid() {
		return ErrInvalidSession
	}
	return nil
}

func sessionBinding(session runtimeapi.SessionRef) string {
	return string(session.WorkspaceID) + "\x00" + string(session.SessionID)
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

var (
	_ runtimeapi.FileQueryAPI = (*FileGitService)(nil)
	_ runtimeapi.GitQueryAPI  = (*FileGitService)(nil)
)
