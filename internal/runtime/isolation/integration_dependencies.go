package isolation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

func (s *Store) registerIntegration(ctx context.Context, original *Entry, i *Integration) error {
	current, unlock, err := s.lockCurrentUsing(ctx, original, s.operationManifest)
	if err != nil {
		return err
	}
	defer unlock()
	if !settleable(current.State) || current.Tree != original.Tree || current.ResumeOwner != i.attempt.Owner {
		return tool.Refusal{Code: CodeStaleExecution, Message: "original result changed before integration registration"}
	}
	if current.Lineage != nil && current.Lineage.Parent != nil {
		_, raw, err := readLineageRecord(entryRecordPath(current))
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		// Older readers must retain the direct parent's environment even after
		// the child settles, until its integration attempts are also settled.
		if resultVersion(current) < 5 {
			fields["version"] = json.RawMessage("5")
		}
		raw, err = json.Marshal(fields)
		if err != nil {
			return err
		}
		if err := fileutil.AtomicWriteFileStrict(entryRecordPath(current), raw, 0600); err != nil {
			return err
		}
	}
	return i.save()
}

func (s *Store) requireSettledIntegrations(e *Entry) error {
	paths, err := s.integrationRecordPaths()
	if err != nil {
		return err
	}
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".json")
		identity, err := readIntegrationIdentity(path, id)
		if err != nil {
			return err
		}
		if identity.ResultID != e.ID {
			continue
		}
		attempt, _, err := s.readIntegration(id)
		if err != nil {
			return err
		}
		if attempt.Owner != e.ResumeOwner || !terminalState(attempt.State) || !attempt.CleanupComplete {
			return dependentResults("unsettled integration attempt retains its source environment and result generation")
		}
		release, err := filelock.TryAcquire(filepath.Join(s.integrationRoot(), id+".lock"))
		if err != nil {
			return dependentResults("integration attempt ownership is active or unknown; source environment retained")
		}
		release()
	}
	return nil
}
