package delegation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reasonix/internal/base/filelock"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/store"
	"strings"
)

const fleetRecoveryLimit = 2 << 20

type fleetRecoveryRecord struct {
	BudgetScope string          `json:"budget_scope,omitempty"`
	Version     int             `json:"version"`
	ID          string          `json:"id"`
	Parent      string          `json:"parent"`
	Workspace   string          `json:"workspace"`
	Tasks       []fleetTaskItem `json:"tasks"`
	Identities  []string        `json:"identities"`
	FailFast    bool            `json:"fail_fast"`
	Attempts    []string        `json:"attempts"`
}

type fleetRecoveryLease struct {
	root    *os.Root
	record  fleetRecoveryRecord
	release func()
}

func (r *fleetRecoveryLease) finish(result *string, handedOff *bool) {
	if r == nil {
		return
	}
	if !*handedOff {
		r.close()
	}
	*result += "\nFleet recovery id: " + r.record.ID
}

func (r *fleetRecoveryLease) close() {
	if r != nil {
		r.release()
		r.root.Close()
	}
}

func (f *FleetTool) recoveryRoot(ctx context.Context, create bool) (*os.Root, error) {
	if agent.SubagentDepth(ctx) > 0 || agent.ParentSession(ctx) == "" {
		return nil, fmt.Errorf("fleet recovery requires the owning persisted root conversation")
	}
	path := f.taskTool.executionSessionPath(ctx)
	if path == "" {
		return nil, fmt.Errorf("fleet recovery requires a persisted parent transcript")
	}
	dir := store.SessionFleetRecoveryDir(path)
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("invalid fleet recovery directory")
	}
	return os.OpenRoot(dir)
}

func validRecoveryID(id string) bool {
	return strings.HasPrefix(id, "fr_") && len(id) < 100 && filepath.IsLocal(id) && filepath.Base(id) == id && !strings.ContainsAny(id, `/\.`)
}

func readFleetRecovery(root *os.Root, id string) (fleetRecoveryRecord, error) {
	var record fleetRecoveryRecord
	if !validRecoveryID(id) {
		return record, fmt.Errorf("invalid fleet recovery id")
	}
	name := id + ".json"
	info, err := root.Lstat(name)
	if err != nil {
		return record, err
	}
	if !info.Mode().IsRegular() || info.Size() > fleetRecoveryLimit {
		return record, fmt.Errorf("invalid fleet recovery record")
	}
	f, err := root.Open(name)
	if err != nil {
		return record, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, fleetRecoveryLimit+1))
	if err != nil {
		return record, err
	}
	if len(data) > fleetRecoveryLimit {
		return record, fmt.Errorf("fleet recovery record exceeds limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&record); err != nil {
		return record, err
	}
	if d.Decode(new(any)) != io.EOF || record.Version != 1 || record.ID != id || len(record.Tasks) < fleetMinTasks || len(record.Tasks) > fleetMaxTasks || len(record.Identities) != len(record.Tasks) || len(record.Attempts) == 0 || len(record.Attempts) > 256 {
		return record, fmt.Errorf("unsupported or malformed fleet recovery record; preserved")
	}
	return record, nil
}

func (r *fleetRecoveryLease) save() error {
	data, err := json.Marshal(r.record)
	if err != nil {
		return err
	}
	if len(data) > fleetRecoveryLimit {
		return fmt.Errorf("fleet recovery record exceeds %d bytes", fleetRecoveryLimit)
	}
	name := r.record.ID + ".tmp-" + rand.Text()
	f, err := r.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = r.root.Remove(name) }()
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := r.root.Rename(name, r.record.ID+".json"); err != nil {
		return err
	}
	if dir, err := r.root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func (f *FleetTool) acquireRecovery(ctx context.Context, id string) (*fleetRecoveryLease, error) {
	if !validRecoveryID(id) {
		return nil, fmt.Errorf("invalid fleet recovery id")
	}
	root, err := f.recoveryRoot(ctx, false)
	if err != nil {
		return nil, err
	}
	release, err := filelock.TryAcquireRoot(root, id+".lock")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("fleet recovery is already active or unavailable: %w", err)
	}
	r := &fleetRecoveryLease{root: root, release: release}
	r.record, err = readFleetRecovery(root, id)
	if err == nil && (r.record.Parent != agent.ParentSession(ctx) || r.record.Workspace != f.taskTool.workspaceRoot) {
		err = fmt.Errorf("fleet recovery belongs to another conversation or workspace")
	}
	if err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}
