package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
)

const resultLockDirectory = ".result-locks-v1"
const resultAdmissionWait = 30 * time.Second

func resultLockName(id string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(id)))
	return hex.EncodeToString(digest[:]) + ".lock"
}

func (s *Store) resultLockRoot() (*os.Root, error) {
	if s == nil {
		return nil, errUnavailable
	}
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return nil, err
	}
	defer managed.Close()
	if err := managed.Mkdir(resultLockDirectory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if !recordDirectoryEntry(managed, resultLockDirectory) {
		return nil, fmt.Errorf("result operation lock namespace is not a directory: %w", os.ErrInvalid)
	}
	return managed.OpenRoot(resultLockDirectory)
}

func (s *Store) acquireResultOperation(ctx context.Context, id string, try bool) (func(), error) {
	root, err := s.resultLockRoot()
	if err != nil {
		return nil, operationAdmissionError(id, err)
	}
	defer root.Close()
	var release func()
	if try {
		release, err = filelock.TryAcquireRoot(root, resultLockName(id))
	} else {
		bounded, cancel := context.WithTimeout(ctx, resultAdmissionWait)
		defer cancel()
		release, err = filelock.AcquireRoot(bounded, root, resultLockName(id))
	}
	if err != nil {
		return nil, operationAdmissionError(id, err)
	}
	// Filelock bounds waiting and may take a free lock after cancellation.
	if err := ctx.Err(); err != nil {
		release()
		return nil, operationAdmissionError(id, err)
	}
	if err := s.checkSettlementFence(id); err != nil {
		release()
		return nil, operationAdmissionError(id, err)
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, operationAdmissionError(id, err)
	}
	return release, nil
}

func operationAdmissionError(id string, err error) error {
	return errors.Join(tool.Refusal{Code: CodeUnavailable,
		Message: fmt.Sprintf("isolated result %s operation was not admitted: %v", strings.TrimSpace(id), err)}, err)
}

func (s *Store) acquireKnownResultOperation(ctx context.Context, id string) (func(), error) {
	if s == nil {
		return nil, errUnavailable
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	known := s.entries[id] != nil || s.uninspected[id] != nil
	s.mu.Unlock()
	var pendingIDs []string
	if !known {
		pendingIDs = s.Pending()
		known = slices.Contains(pendingIDs, id)
	}
	if !known {
		known = s.resultLockExists(id)
	}
	if !known {
		return nil, unknownResultError(id, pendingIDs)
	}
	return s.acquireResultOperation(ctx, id, false)
}

func unknownResultError(id string, pending []string) error {
	known := "none are pending"
	if len(pending) > 0 {
		known = "pending: " + strings.Join(pending, ", ")
	}
	return tool.Refusal{Code: CodeUnknownID, Message: fmt.Sprintf("no isolated result %q; %s", id, known)}
}

func (s *Store) resultLockExists(id string) bool {
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return false
	}
	defer managed.Close()
	if !recordDirectoryEntry(managed, resultLockDirectory) {
		return false
	}
	root, err := managed.OpenRoot(resultLockDirectory)
	if err != nil {
		return false
	}
	defer root.Close()
	return recordRegularFile(root, resultLockName(id))
}

func (s *Store) rememberUnadmittedFinish(e *Entry, cause error) error {
	s.mu.Lock()
	s.uninspected[e.ID] = e
	s.mu.Unlock()
	return fmt.Errorf("isolated result %s files retained; operation admission failed, only this live store can retry: %w", e.ID, cause)
}
