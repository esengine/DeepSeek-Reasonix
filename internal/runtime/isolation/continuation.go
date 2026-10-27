package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The existing settlement fence makes preceding readers preserve resumable work.
type continuationRecord struct {
	Kind    string       `json:"kind"`
	Version int          `json:"version"`
	Owner   string       `json:"owner"`
	Entry   durableEntry `json:"entry"`
}

func continuationName(id string) string {
	return strings.TrimSuffix(resultLockName(id), ".lock") + ".json"
}

func (s *Store) continuationRoot(create bool) (*os.Root, error) {
	root, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if create {
		if err := root.Mkdir(settlementFenceDirectory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	if !recordDirectoryEntry(root, settlementFenceDirectory) {
		return nil, fmt.Errorf("invalid continuation namespace")
	}
	return root.OpenRoot(settlementFenceDirectory)
}

func (s *Store) readContinuation(id string) (continuationRecord, bool) {
	root, err := s.continuationRoot(false)
	if err != nil {
		return continuationRecord{}, false
	}
	defer root.Close()
	return s.readContinuationFrom(root, continuationName(id), id)
}

func (s *Store) readContinuationFrom(root *os.Root, name, id string) (continuationRecord, bool) {
	var record continuationRecord
	if !recordRegularFile(root, name) {
		return record, false
	}
	f, err := root.Open(name)
	if err != nil {
		return record, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return record, false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&record) != nil || d.Decode(new(any)) != io.EOF || record.Kind != "subagent-continuation" || record.Version != 1 || record.Owner == "" || record.Entry.ID != id {
		return record, false
	}
	managed, err := os.OpenRoot(s.managedRoot)
	if err != nil {
		return record, false
	}
	defer managed.Close()
	local, err := filepath.Rel(s.managedRoot, filepath.Dir(record.Entry.Cand.WorktreeRoot))
	if err != nil || !filepath.IsLocal(local) || filepath.Dir(local) != "." || !recordDirectoryEntry(managed, local) {
		return record, false
	}
	slot, err := managed.OpenRoot(local)
	if err != nil {
		return record, false
	}
	defer slot.Close()
	entry, ok := s.bindRecord(slot, record.Entry)
	record.Entry = entry
	if ok && s.checkCachedInspection(&Entry{ID: entry.ID, Workspace: entry.Workspace, Snap: entry.Snap, Cand: entry.Cand}) != nil {
		return record, false
	}
	return record, ok
}

func (s *Store) recordContinuation(e *Entry, owner string) error {
	root, err := s.continuationRoot(true)
	if err != nil {
		return err
	}
	defer root.Close()
	data, err := json.Marshal(continuationRecord{Kind: "subagent-continuation", Version: 1, Owner: owner, Entry: entryIdentity(e)})
	if err != nil {
		return err
	}
	f, err := root.OpenFile(continuationName(e.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if dir, err := root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return errors.Join(writeErr, syncErr, closeErr)
}

func (s *Store) continuationIDs() []string {
	root, err := s.continuationRoot(false)
	if err != nil {
		return nil
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil
	}
	var ids []string
	for _, entry := range entries {
		if !recordRegularFile(root, entry.Name()) {
			continue
		}
		f, err := root.Open(entry.Name())
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		f.Close()
		if err != nil || len(data) > 64<<10 {
			continue
		}
		var candidate continuationRecord
		if json.Unmarshal(data, &candidate) != nil || continuationName(candidate.Entry.ID) != entry.Name() {
			continue
		}
		if r, ok := s.readContinuationFrom(root, entry.Name(), candidate.Entry.ID); ok {
			ids = append(ids, r.Entry.ID)
		}
	}
	return ids
}

// ExecuteContinuation holds settlement ownership for the entire resumed run.
func (s *Store) ExecuteContinuation(ctx context.Context, runner Runner, workspace, prompt, model, owner, id string) (string, error) {
	if runner == nil {
		return "", errUnavailable
	}
	if owner == "" {
		return s.Execute(ctx, runner, workspace, prompt, model)
	}
	var e *Entry
	var release func()
	var err error
	if id == "" {
		e, err = s.Begin(ctx, workspace)
		if err != nil {
			return "", err
		}
		release, err = s.acquireResultOperation(ctx, e.ID, false)
		if err != nil {
			return "", s.rememberUnadmittedFinish(e, err)
		}
		defer release()
		if err = s.recordContinuation(e, owner); err != nil {
			return "", s.retainInspection(e, fmt.Errorf("record continuation: %w", err))
		}
	} else {
		release, err = s.acquireKnownResultOperation(ctx, id)
		if err != nil {
			return "", err
		}
		defer release()
		record, ok := s.readContinuation(id)
		if !ok || record.Owner != owner {
			return "", fmt.Errorf("isolated continuation does not belong to this conversation")
		}
		e, err = s.lookupLocked(ctx, id)
		if err != nil {
			return "", err
		}
	}
	out, runErr := invokeRunner(ctx, runner, Run{ID: e.ID, WorkspaceRoot: e.Cand.WorkspaceRoot, Prompt: prompt, Model: model})
	if errors.Is(runErr, ErrRunnerPanic) {
		return "", s.retainInspection(e, runErr)
	}
	kept, finishErr := s.finishLocked(context.WithoutCancel(ctx), e)
	if finishErr != nil {
		return "", fmt.Errorf("isolated task inspection is incomplete; files retained as %s: %w", e.ID, errors.Join(runErr, finishErr))
	}
	if runErr != nil {
		return report(e, kept, out), fmt.Errorf("isolated task %s stopped; its work is retained: %w", e.ID, runErr)
	}
	return report(e, kept, out), nil
}
