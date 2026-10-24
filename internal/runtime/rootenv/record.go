package rootenv

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"reasonix/internal/base/fileutil"
)

func validID(id string) bool {
	text, ok := strings.CutPrefix(id, "renv_")
	if !ok || text == "" {
		return false
	}
	for _, c := range text {
		if c < 'A' || c > 'Z' {
			if c < '2' || c > '7' {
				return false
			}
		}
	}
	return true
}

func (s *Store) environmentDir(id string) string { return filepath.Join(s.dir, id) }
func (s *Store) recordPath(id string) string {
	return filepath.Join(s.environmentDir(id), "environment-v1.json")
}
func (s *Store) livePath(id string) string {
	return filepath.Join(s.environmentDir(id), "execution.lock")
}

func (s *Store) load(id string) (Record, error) {
	if !validID(id) {
		return Record{}, ErrProtocol
	}
	raw, err := os.ReadFile(s.recordPath(id))
	if err != nil {
		return Record{}, err
	}
	var r Record
	if json.Unmarshal(raw, &r) != nil || !supportedRecord(r) {
		return Record{}, ErrProtocol
	}
	if r.ID != id || r.Generation == "" || r.RunGeneration == "" || r.SessionID == "" ||
		r.SourceWorkspace != s.workspace || r.Source != s.repo {
		return Record{}, ErrProtocol
	}
	if !validPublication(r) {
		return Record{}, ErrProtocol
	}
	switch r.State {
	case "preparing", "materializing", "ready", "recovery_required":
	default:
		return Record{}, ErrProtocol
	}
	return r, nil
}

func (s *Store) verifyCandidate(r Record) error {
	if err := r.Source.Verify(); err != nil {
		return err
	}
	c := r.Candidate
	parent, err := fileutil.CanonicalWritePath(s.environmentDir(r.ID))
	if err != nil {
		return err
	}
	root, err := fileutil.CanonicalWritePath(c.WorktreeRoot)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(parent, root)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return ErrProtocol
	}
	if c.Repo.WorkTree != root || c.Repo.CommonDir != r.Source.CommonDir || !c.Repo.Valid() {
		return ErrProtocol
	}
	if err := c.Repo.Verify(); err != nil {
		return err
	}
	if r.Baseline.Repo != r.Source.Top() || r.Baseline.Commit == "" || r.Cursor.Commit == "" {
		return ErrProtocol
	}
	prefix := filepath.FromSlash(r.Baseline.Prefix)
	if prefix != "" && (!filepath.IsLocal(prefix) || filepath.Clean(prefix) != prefix) {
		return ErrProtocol
	}
	expected := filepath.Join(root, prefix)
	workspace, err := filepath.EvalSymlinks(c.WorkspaceRoot)
	if err != nil {
		return err
	}
	repoDir, err := fileutil.CanonicalWritePath(c.Repo.Dir)
	if err != nil {
		return err
	}
	if filepath.Clean(workspace) != expected || repoDir != root {
		return ErrProtocol
	}
	info, err := os.Stat(c.WorkspaceRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrProtocol
	}
	return nil
}

func (s *Store) save(r Record) error {
	if !validID(r.ID) || !supportedRecord(r) {
		return ErrProtocol
	}
	fields := map[string]json.RawMessage{}
	var previous *Record
	if raw, err := os.ReadFile(s.recordPath(r.ID)); err == nil {
		old, err := s.load(r.ID)
		if err != nil {
			return err
		}
		if old.Generation != r.Generation || old.SessionID != r.SessionID {
			return ErrStale
		}
		if old.Version != r.Version || !reflect.DeepEqual(old.CheckContract, r.CheckContract) {
			return ErrProtocol
		}
		previous = &old
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	updates := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &updates); err != nil {
		return err
	}
	if previous != nil {
		old := reflect.ValueOf(*previous)
		next := reflect.ValueOf(r)
		typ := old.Type()
		for n := range old.NumField() {
			if reflect.DeepEqual(old.Field(n).Interface(), next.Field(n).Interface()) {
				delete(updates, strings.Split(typ.Field(n).Tag.Get("json"), ",")[0])
			}
		}
	}
	for _, key := range []string{"publication", "cursor"} {
		if key == "publication" && previous != nil &&
			(previous.Publication == nil || r.Publication == nil || previous.Publication.ID != r.Publication.ID) {
			continue
		}
		previous, next := fields[key], updates[key]
		if len(previous) > 0 && previous[0] == '{' && len(next) > 0 && next[0] == '{' {
			updates[key], err = mergeRecordObject(previous, next)
			if err != nil {
				return err
			}
		}
	}
	maps.Copy(fields, updates)
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(s.recordPath(r.ID), raw, 0600)
}

func mergeRecordObject(previous, next json.RawMessage) (json.RawMessage, error) {
	old, updates := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	if err := json.Unmarshal(previous, &old); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(next, &updates); err != nil {
		return nil, err
	}
	for key, value := range updates {
		prior := old[key]
		if len(prior) > 0 && prior[0] == '{' && len(value) > 0 && value[0] == '{' {
			merged, err := mergeRecordObject(prior, value)
			if err != nil {
				return nil, err
			}
			value = merged
		}
		old[key] = value
	}
	return json.Marshal(old)
}
