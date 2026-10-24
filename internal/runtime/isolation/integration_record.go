package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
)

func (s *Store) integrationRoot() string { return filepath.Join(s.managedRoot, ".integration-v1") }

func (s *Store) integrationPath(id string) (string, error) {
	if !strings.HasPrefix(id, "int_") || len(id) != 30 || strings.ContainsAny(id, "/\\.\x00") {
		return "", tool.Refusal{Code: CodeUnknownID, Message: "invalid integration attempt identity"}
	}
	for _, letter := range id[4:] {
		if (letter < 'A' || letter > 'Z') && (letter < '2' || letter > '7') {
			return "", tool.Refusal{Code: CodeUnknownID, Message: "invalid integration attempt identity"}
		}
	}
	return filepath.Join(s.integrationRoot(), id+".json"), nil
}

func (s *Store) readIntegration(id string) (IntegrationAttempt, []byte, error) {
	path, err := s.integrationPath(id)
	if err != nil {
		return IntegrationAttempt{}, nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return IntegrationAttempt{}, nil, err
	}
	var attempt IntegrationAttempt
	if err := json.Unmarshal(raw, &attempt); err != nil {
		return attempt, raw, fmt.Errorf("integration record retained: %w", err)
	}
	if attempt.Version != 1 || attempt.ID != id || !knownState(attempt.State) || attempt.Source.Repo != s.repo.Top() ||
		!sameDir(attempt.Source.RepoRoot, s.repo.WorkTree) || !validID(attempt.ResultID) ||
		!worktree.IsManagedPath(attempt.Candidate.WorktreeRoot, s.integrationRoot()) {
		return attempt, raw, tool.Refusal{Code: CodeUnsupportedState, Message: "unsupported integration record identity; original file retained"}
	}
	if !validIntegrationLifecycle(attempt) {
		return attempt, raw, tool.Refusal{Code: CodeUnsupportedState, Message: "invalid integration lifecycle; original file retained"}
	}
	return attempt, raw, nil
}

func validIntegrationLifecycle(attempt IntegrationAttempt) bool {
	if (attempt.Environment != nil && attempt.Environment.Version != 1) || !supportedDiscardContent(durableEntry{State: attempt.State, DiscardContent: attempt.DiscardContent, DiscardApproval: attempt.DiscardApproval}) {
		return false
	}
	if !knownState(attempt.State) || attempt.State == "" || (attempt.CleanupComplete && !terminalState(attempt.State)) ||
		(attempt.State == Integrated && (attempt.Tree == "" || attempt.Verification != "valid")) {
		return false
	}
	if attempt.Verification != "unknown" && attempt.Verification != "invalid" && attempt.Verification != "valid" {
		return false
	}
	return attempt.Tree != "" || attempt.State == Preparing || attempt.State == RecoveryRequired || attempt.State == Conflict || attempt.State == Discarded
}

func (i *Integration) save() error {
	if !validIntegrationLifecycle(i.attempt) {
		return tool.Refusal{Code: CodeUnsupportedState, Message: "invalid integration transition; original file retained"}
	}
	path, err := i.store.integrationPath(i.attempt.ID)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(i.attempt)
	if err != nil {
		return err
	}
	previous, oldRaw, err := i.store.readIntegration(i.attempt.ID)
	if err == nil {
		before, after := previous, i.attempt
		before.State, after.State = "", ""
		before.Tree, after.Tree = "", ""
		before.Changes, after.Changes = nil, nil
		before.Content, after.Content = i.attempt.Content, i.attempt.Content
		before.Checks, after.Checks = nil, nil
		before.CheckContracts, after.CheckContracts = nil, nil
		before.Verification, after.Verification = "", ""
		before.Reason, after.Reason = "", ""
		before.CleanupComplete, after.CleanupComplete = false, false
		before.DiscardContent, after.DiscardContent = nil, nil
		before.DiscardApproval, after.DiscardApproval = "", ""
		if previous.State == Preparing && before.Candidate.Repo.Dir == "" {
			before.Candidate.Repo = after.Candidate.Repo
			before.Environment = after.Environment
		}
		terminalBefore, terminalAfter := previous, i.attempt
		terminalBefore.CleanupComplete, terminalAfter.CleanupComplete = false, false
		if previous.State == Discarded && !previous.CleanupComplete {
			terminalBefore.DiscardContent, terminalAfter.DiscardContent = nil, nil
			terminalBefore.DiscardApproval, terminalAfter.DiscardApproval = "", ""
		}
		if !reflect.DeepEqual(before, after) || (previous.State != Preparing && (previous.Tree != i.attempt.Tree || !reflect.DeepEqual(previous.Changes, i.attempt.Changes))) ||
			(terminalState(previous.State) && (!reflect.DeepEqual(terminalBefore, terminalAfter) || (previous.CleanupComplete && !i.attempt.CleanupComplete))) {
			return tool.Refusal{Code: CodeStaleExecution, Message: "integration identity or terminal decision changed; original record retained"}
		}
		raw, err = mergeIdentityFields(oldRaw, raw)
		if err != nil {
			return err
		}
		// Preserve extension fields in observations that did not change. Arrays
		// cannot use the recursive object merge used for immutable identities.
		var oldFields, fields map[string]json.RawMessage
		if err := json.Unmarshal(oldRaw, &oldFields); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		if reflect.DeepEqual(previous.Checks, i.attempt.Checks) {
			fields["checks"] = oldFields["checks"]
		}
		if reflect.DeepEqual(previous.CheckContracts, i.attempt.CheckContracts) {
			fields["check_contracts"] = oldFields["check_contracts"]
		}
		if reflect.DeepEqual(previous.Changes, i.attempt.Changes) {
			fields["changes"] = oldFields["changes"]
		}
		raw, err = json.Marshal(fields)
		if err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, raw, 0600)
}

// OpenIntegration recovers ownership only, never the claim that a stopped
// check kept running. Unknown versions and malformed records remain untouched.
func (s *Store) OpenIntegration(ctx context.Context, id, owner string) (*Integration, error) {
	return s.openIntegration(ctx, id, owner, true)
}

func (s *Store) openIntegration(ctx context.Context, id, owner string, reconcile bool) (*Integration, error) {
	if s == nil || s.mutations == nil {
		return nil, errUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := s.integrationPath(id); err != nil {
		return nil, err
	}
	release, err := filelock.TryAcquire(filepath.Join(s.integrationRoot(), id+".lock"))
	if err != nil {
		return nil, err
	}
	attempt, _, err := s.readIntegration(id)
	if err == nil && attempt.Owner != owner {
		err = tool.Refusal{Code: CodeOwnerMismatch, Message: "integration attempt belongs to another execution owner"}
	}
	if err != nil {
		release()
		return nil, err
	}
	i := &Integration{store: s, attempt: attempt, release: release}
	if reconcile && attempt.State == Running {
		i.attempt.State, i.attempt.Verification, i.attempt.Reason = Interrupted, "unknown", "verification was interrupted; rerun checks"
		if err := i.save(); err != nil {
			i.Close()
			return nil, err
		}
	}
	if reconcile && attempt.State == Preparing {
		i.attempt.State, i.attempt.Verification, i.attempt.Reason = RecoveryRequired, "unknown", "integration preparation was interrupted; candidate retained"
		if err := i.save(); err != nil {
			i.Close()
			return nil, err
		}
	}
	return i, nil
}
