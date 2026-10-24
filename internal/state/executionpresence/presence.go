// Package executionpresence atomically selects root execution environments.
// Its liveness locks register work; they never grant exclusive file access.
package executionpresence

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
)

var ErrProtocol = errors.New("unsupported execution presence record; original retained")
var ErrStale = errors.New("execution presence generation changed")

type Strategy string

const (
	Auto     Strategy = "auto"
	Shared   Strategy = "shared"
	Isolated Strategy = "isolated"
)

type Request struct {
	Strategy          Strategy
	Writer            bool
	SessionID         string
	RootExecutionID   string
	ParentExecutionID string
}

type Record struct {
	Version           int      `json:"version"`
	ID                string   `json:"id"`
	Generation        string   `json:"generation"`
	WorkingCopy       string   `json:"working_copy"`
	ActualWorkingCopy string   `json:"actual_working_copy"`
	SessionID         string   `json:"session_id"`
	RootExecutionID   string   `json:"root_execution_id"`
	ParentExecutionID string   `json:"parent_execution_id,omitempty"`
	Writer            bool     `json:"writer"`
	Requested         Strategy `json:"requested"`
	Selected          Strategy `json:"selected"`
	Phase             string   `json:"phase"`
	Reason            string   `json:"reason,omitempty"`
}

type Store struct{ root, dir string }

// New takes the host-discovered working-copy root, rather than Git common dir.
// Different selected subdirectories of one worktree use that same root.
func New(workingCopyRoot, stateDir string) (*Store, error) {
	if !filepath.IsAbs(workingCopyRoot) || !filepath.IsAbs(stateDir) {
		return nil, ErrProtocol
	}
	root, err := fileutil.CanonicalWritePath(workingCopyRoot)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrProtocol
	}
	dir := filepath.Join(stateDir, "execution-presence-v1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{root: root, dir: dir}, nil
}

type Reservation struct {
	mu      sync.Mutex
	store   *Store
	record  Record
	release func()
}

func (s *Store) Reserve(ctx context.Context, request Request) (*Reservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Strategy != Auto && request.Strategy != Shared && request.Strategy != Isolated {
		return nil, ErrProtocol
	}
	r := Record{Version: 1, ID: rand.Text(), Generation: rand.Text(), WorkingCopy: s.root,
		SessionID: request.SessionID, RootExecutionID: request.RootExecutionID, ParentExecutionID: request.ParentExecutionID,
		Writer: request.Writer, Requested: request.Strategy, Selected: Shared, Phase: "preparing"}
	if r.RootExecutionID == "" {
		r.RootExecutionID = r.ID
	}
	unlock, err := filelock.Acquire(ctx, filepath.Join(s.dir, "registry.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	records, err := s.records()
	if err != nil {
		return nil, err
	}
	if r.Writer {
		if request.Strategy == Isolated {
			r.Selected = Isolated
		} else if request.Strategy == Auto {
			for _, other := range records {
				occupied := other.WorkingCopy
				if other.Phase == "running" {
					occupied = other.ActualWorkingCopy
				} else if other.Selected == Isolated {
					continue
				}
				if other.Writer && sameRoot(occupied, r.WorkingCopy) {
					r.Selected, r.Reason = Isolated, "another writer is active in this working copy"
					break
				}
			}
		}
	}
	release, err := filelock.TryAcquire(s.livePath(r))
	if err != nil {
		return nil, err
	}
	if err := s.saveNew(r); err != nil {
		release()
		_ = os.Remove(s.livePath(r))
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(s.recordPath(r.ID))
		release()
		_ = os.Remove(s.livePath(r))
		return nil, err
	}
	return &Reservation{store: s, record: r, release: release}, nil
}

func (r *Reservation) Record() Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.record
}

// Activate takes the host-discovered actual working-copy root, not tool cwd.
// Lineage is attribution only; relatives remain independent potential writers.
func (r *Reservation) Activate(ctx context.Context, actualWorkingCopyRoot string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	root, err := fileutil.CanonicalWritePath(actualWorkingCopyRoot)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(actualWorkingCopyRoot) || (r.record.Selected == Shared) != sameRoot(root, r.record.WorkingCopy) {
		return ErrProtocol
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrProtocol
	}
	if r.record.Phase != "preparing" {
		if r.record.Phase != "running" || !sameRoot(r.record.ActualWorkingCopy, root) {
			return ErrStale
		}
	}
	return r.update(ctx, func(next *Record) { next.ActualWorkingCopy, next.Phase = root, "running" })
}

func (r *Reservation) FallbackShared(ctx context.Context, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record.Requested != Auto || r.record.Phase != "preparing" || strings.TrimSpace(reason) == "" {
		return ErrProtocol
	}
	return r.update(ctx, func(next *Record) { next.Selected, next.Reason = Shared, reason })
}

func (r *Reservation) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.release == nil {
		return nil
	}
	defer func() {
		r.release()
		r.release = nil
		_ = os.Remove(r.store.livePath(r.record))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, filepath.Join(r.store.dir, "registry.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	current, _, err := r.store.read(r.record.ID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Generation != r.record.Generation {
		return ErrStale
	}
	return os.Remove(r.store.recordPath(r.record.ID))
}

func sameRoot(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
