package isolation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/tool"
)

func (s *Store) markerPaths() ([]string, error) {
	entries, err := os.ReadDir(s.managedRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		for _, name := range []string{entryFile, legacyEntryFile} {
			path := filepath.Join(s.managedRoot, entry.Name(), name)
			if info, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, err
			} else if !info.Mode().IsRegular() {
				return nil, tool.Refusal{Code: CodeUnsupportedState, Message: "non-regular isolation marker retained"}
			}
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func (s *Store) integrationRecordPaths() ([]string, error) {
	entries, err := os.ReadDir(s.integrationRoot())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "int_") && strings.HasSuffix(entry.Name(), ".json") {
			if !entry.Type().IsRegular() {
				return nil, tool.Refusal{Code: CodeUnsupportedState, Message: "non-regular integration marker retained"}
			}
			paths = append(paths, filepath.Join(s.integrationRoot(), entry.Name()))
		}
	}
	return paths, nil
}

func (s *Store) resultRecords() ([]storedResult, error) {
	paths, err := s.markerPaths()
	if err != nil {
		return nil, err
	}
	var records []storedResult
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var d durableEntry
		if json.Unmarshal(raw, &d) != nil || !validID(d.ID) {
			return nil, tool.Refusal{Code: CodeUnsupportedState, Message: "unidentified isolated result retained; identity uniqueness cannot be established"}
		}
		d.legacy = filepath.Base(path) == legacyEntryFile
		records = append(records, storedResult{entry: d, path: path, raw: raw})
	}
	return records, nil
}

func readIntegrationIdentity(path, id string) (IntegrationAttempt, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return IntegrationAttempt{}, err
	}
	if !info.Mode().IsRegular() {
		return IntegrationAttempt{}, tool.Refusal{Code: CodeUnsupportedState, Message: "non-regular integration marker retained"}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return IntegrationAttempt{}, err
	}
	var attempt IntegrationAttempt
	if json.Unmarshal(raw, &attempt) != nil || attempt.ID != id || !validID(attempt.ResultID) {
		return attempt, tool.Refusal{Code: CodeUnsupportedState, Message: "unidentified integration attempt retained; dependency ownership cannot be established"}
	}
	return attempt, nil
}
