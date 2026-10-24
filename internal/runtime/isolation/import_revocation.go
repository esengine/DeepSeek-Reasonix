package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

type importRevocation struct {
	Version int            `json:"version"`
	Request SnapshotImport `json:"request"`
}

func (s *Store) revocationPath(id string) string {
	return filepath.Join(s.managedRoot, "."+id+".revocation-v1.json")
}

// RevokeImportedResult fences every source publisher at the result entry lock.
// A failed owner update leaves a durable revocation; repeating it is idempotent.
func (s *Store) RevokeImportedResult(ctx context.Context, request SnapshotImport, complete func() error) error {
	unlock, err := s.lockEntry(ctx, request.ID)
	if err != nil {
		return err
	}
	defer unlock()
	e, err := s.operationManifest(request.ID)
	if err == nil {
		if !matchesSnapshotImport(e, request) {
			return tool.Refusal{Code: CodeOwnerMismatch, Message: "revocation does not own this publication"}
		}
		if e.State == Applying || e.State == Integrated {
			return tool.Refusal{Code: CodeRecoveryRequired, Message: "publication reached source; reconcile its receipt before retrying"}
		}
	} else {
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != CodeUnknownID {
			return err
		}
	}
	record := importRevocation{Version: 1, Request: request}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(s.revocationPath(request.ID)); err == nil {
		var previous importRevocation
		if json.Unmarshal(existing, &previous) != nil || !reflect.DeepEqual(previous, record) {
			return tool.Refusal{Code: CodeUnsupportedState, Message: "unrecognized publication revocation retained"}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := fileutil.AtomicCreateFile(s.revocationPath(request.ID), raw, 0600); err != nil {
			return err
		}
	} else {
		return err
	}
	return complete()
}

func (s *Store) checkImportedPublication(e *Entry) error {
	if e.Origin == nil {
		return nil
	}
	if _, err := os.Lstat(s.revocationPath(e.ID)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return tool.Refusal{Code: CodeStaleExecution, Message: "root publication was revoked; retained result can be inspected or discarded"}
}
