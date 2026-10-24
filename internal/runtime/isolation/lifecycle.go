package isolation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
)

type State string

const (
	Preparing        State = "preparing"
	Running          State = "running"
	Pending          State = "pending"
	Applying         State = "applying"
	Integrated       State = "integrated"
	Discarded        State = "discarded"
	RecoveryRequired State = "recovery_required"
	Interrupted      State = "interrupted"
	Failed           State = "failed"
	Cancelled        State = "cancelled"
	Conflict         State = "conflict"
)

const CodeBusy = "isolation.busy"
const CodeRecoveryRequired = "isolation.recovery_required"
const CodeUnsupportedState = "isolation.unsupported_state"
const CodeStaleExecution = "isolation.stale_execution"
const CodeOwnerMismatch = "isolation.owner_mismatch"
const CodeAlreadySettled = "isolation.already_settled"

func knownState(state State) bool {
	switch state {
	case "", Preparing, Running, Pending, Applying, Integrated, Discarded, RecoveryRequired, Interrupted, Failed, Cancelled, Conflict:
		return true
	default:
		return false
	}
}

func terminalState(state State) bool { return state == Integrated || state == Discarded }

func supportedLifecycle(d durableEntry) bool {
	return knownState(d.State) && (!d.CleanupComplete || terminalState(d.State)) && (d.State != Integrated || d.Tree != "") &&
		(d.Environment == nil || d.Environment.Version == 1) && (d.DiscardPolicy == "" || d.DiscardPolicy == hostDiscardPolicy) && supportedDiscardContent(d)
}

func settleable(state State) bool {
	return state == Pending || state == Failed || state == Cancelled || state == Conflict
}

func validID(id string) bool {
	if !strings.HasPrefix(id, "iso_") || len(id) != 12 {
		return false
	}
	_, err := hex.DecodeString(id[4:])
	return err == nil
}

func (s *Store) lockEntry(ctx context.Context, id string) (func(), error) {
	if s == nil {
		return nil, errUnavailable
	}
	if !validID(id) {
		return nil, tool.Refusal{Code: CodeUnknownID, Message: "invalid isolated result id"}
	}
	if err := os.MkdirAll(s.managedRoot, 0700); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, filepath.Join(s.managedRoot, "."+id+".lock"))
}

func (s *Store) runLock(id string) string { return filepath.Join(s.managedRoot, "."+id+".run.lock") }

func (s *Store) startEntry(e *Entry) error {
	var generation [16]byte
	if _, err := rand.Read(generation[:]); err != nil {
		return err
	}
	release, err := filelock.TryAcquire(s.runLock(e.ID))
	if err != nil {
		return err
	}
	e.releaseRun = release
	e.RunGeneration = hex.EncodeToString(generation[:])
	if err := persist(e); err != nil {
		s.endRun(e)
		return err
	}
	return nil
}

func (s *Store) checkRunGeneration(e *Entry) error {
	d, err := s.managedResultMetadata(e.ID)
	if err != nil {
		return err
	}
	if !supportedResultVersion(d) || d.Version == 0 || !supportedLifecycle(d) {
		return tool.Refusal{Code: CodeUnsupportedState, Message: "isolated result was written by a newer protocol"}
	}
	if terminalState(d.State) {
		return tool.Refusal{Code: CodeAlreadySettled, Message: "isolated execution has already settled"}
	}
	if e.RunGeneration == "" || d.RunGeneration != e.RunGeneration || d.ID != e.ID || d.Snap != e.Snap || d.Cand != e.Cand || d.ResumeOwner != e.ResumeOwner || !reflect.DeepEqual(d.Origin, e.Origin) {
		return tool.Refusal{Code: CodeStaleExecution, Message: "isolated execution generation changed; result retained"}
	}
	e.Lineage = d.Lineage
	return nil
}

func (s *Store) endRun(e *Entry) {
	if e.releaseRun != nil {
		e.releaseRun()
		e.releaseRun = nil
	}
}
