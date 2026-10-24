package isolation

import (
	"bytes"
	"errors"
	"path/filepath"

	"reasonix/internal/contract/tool"
)

type storedResult struct {
	entry durableEntry
	path  string
	raw   []byte
}

func (s *Store) scopedResultMetadata(id string) (durableEntry, error) {
	d, err := s.managedResultMetadata(id)
	var refusal tool.Refusal
	if errors.As(err, &refusal) && refusal.Code == CodeOwnerMismatch {
		return durableEntry{}, tool.Refusal{Code: CodeUnknownID, Message: "isolated result is unavailable in this workspace"}
	}
	return d, err
}

// Identity is unique across the managed root, before workspace filtering.
// An interrupted terminal promotion may retain two byte-identical markers.
func (s *Store) managedResultMetadata(id string) (durableEntry, error) {
	var records []storedResult
	all, err := s.resultRecords()
	if err != nil {
		return durableEntry{}, err
	}
	for _, record := range all {
		if record.entry.ID != id {
			continue
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return durableEntry{}, tool.Refusal{Code: CodeUnknownID, Message: "original isolated result identity is unavailable"}
	}
	if len(records) > 1 && (len(records) != 2 || !terminalPromotionPair(records[0], records[1])) {
		return durableEntry{}, tool.Refusal{Code: CodeUnsupportedState, Message: "duplicate original result identities retained"}
	}
	found := records[0].entry
	if len(records) == 2 && found.legacy {
		found = records[1].entry
	}
	if !sameDir(found.Workspace, s.workspace) {
		return durableEntry{}, tool.Refusal{Code: CodeOwnerMismatch, Message: "original result belongs to another workspace"}
	}
	if !validID(found.ID) || !supportedResultVersion(found) || !supportedLifecycle(found) {
		return durableEntry{}, tool.Refusal{Code: CodeUnsupportedState, Message: "original result identity is unsupported; record retained"}
	}
	return found, nil
}

func terminalPromotionPair(a, b storedResult) bool {
	return a.entry.legacy != b.entry.legacy && sameDir(filepath.Dir(a.path), filepath.Dir(b.path)) &&
		terminalState(a.entry.State) && a.entry.Version != 0 && supportedResultVersion(a.entry) && supportedLifecycle(a.entry) && bytes.Equal(a.raw, b.raw)
}
