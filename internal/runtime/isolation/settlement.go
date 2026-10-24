package isolation

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

// Only terminal legacy records migrate; an active candidate has one protocol owner.
func (s *Store) promoteSettled(e *Entry) error {
	legacyPath := filepath.Join(filepath.Dir(e.Cand.WorktreeRoot), legacyEntryFile)
	raw, err := os.ReadFile(legacyPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		var old durableEntry
		if err := json.Unmarshal(raw, &old); err != nil {
			return err
		}
		if !supportedResultVersion(old) || !supportedLifecycle(old) || !terminalState(old.State) || old.Workspace != e.Workspace ||
			old.ResumeOwner != e.ResumeOwner || old.ID != e.ID || old.State != e.State || old.Tree != e.Tree || old.Snap != e.Snap || old.Cand != e.Cand ||
			old.RunGeneration != e.RunGeneration || !slices.Equal(old.Changes, e.Changes) {
			return tool.Refusal{Code: CodeUnsupportedState, Message: "legacy result differs from terminal receipt; files retained"}
		}
		if e.legacy {
			if _, err := os.Lstat(entryPath(e.Cand)); !errors.Is(err, os.ErrNotExist) {
				return tool.Refusal{Code: CodeUnsupportedState, Message: "terminal receipt already exists; reload before cleanup"}
			}
			if err := fileutil.AtomicWriteFile(entryPath(e.Cand), raw, 0600); err != nil {
				return err
			}
			e.legacy = false
		}
		current, err := os.ReadFile(entryPath(e.Cand))
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, current) {
			return tool.Refusal{Code: CodeUnsupportedState, Message: "legacy receipt changed during migration; both records retained"}
		}
		if s.promotionProbe != nil {
			if err := s.promotionProbe(e); err != nil {
				return err
			}
		}
		if err := os.Remove(legacyPath); err != nil {
			return err
		}
	}
	if e.legacy {
		return tool.Refusal{Code: CodeUnsupportedState, Message: "legacy terminal receipt is missing; candidate retained"}
	}
	f, err := os.OpenFile(filepath.Join(s.managedRoot, "."+e.ID+".allocated"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}
