package agent

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"reasonix/internal/base/fileutil"
)

// scanReaders bounds live walk workers, including directory and metadata reads.
const scanReaders = 16

// scanWorkspaceTo answers what a sequential filepath.WalkDir would: links are
// not entered, VCS stores below the root are skipped, an unreadable entry or a
// stop leaves it incomplete, and more than limit files put it over the limit.
// The limit is an argument so a test reaches that answer with a small tree.
func scanWorkspaceTo(ctx context.Context, root string, limit int) workspaceScan {
	if root == "" {
		return workspaceScan{}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return workspaceScan{}
	}
	info, err := os.Lstat(root)
	if err != nil {
		return workspaceScan{state: map[string]pathState{}}
	}
	if !info.IsDir() {
		state := map[string]pathState{}
		if limit > 0 {
			state[root] = pathState{exists: true, size: info.Size(), modTime: info.ModTime().UnixNano(), mode: info.Mode()}
		}
		return workspaceScan{state: state, complete: limit > 0, overLimit: limit <= 0}
	}
	w := &scanWalk{ctx: ctx, limit: int64(limit), state: make(map[string]pathState, 4096), jobs: make(chan string)}
	var workers sync.WaitGroup
	for range scanReaders - 1 {
		workers.Go(func() {
			for path := range w.jobs {
				w.dir(path)
				w.wg.Done()
			}
		})
	}
	w.dir(root)
	w.wg.Wait()
	close(w.jobs)
	workers.Wait()
	return workspaceScan{state: w.state, complete: !w.short.Load(), overLimit: w.over.Load()}
}

type scanWalk struct {
	ctx   context.Context
	limit int64
	files atomic.Int64
	short atomic.Bool // the walk stopped or skipped something
	over  atomic.Bool
	jobs  chan string
	wg    sync.WaitGroup
	mu    sync.Mutex
	state map[string]pathState
}

func (w *scanWalk) stopped() bool {
	if w.over.Load() {
		return true
	}
	if w.ctx.Err() != nil {
		w.short.Store(true)
		return true
	}
	return false
}

func (w *scanWalk) dir(path string) {
	if w.stopped() {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		w.short.Store(true)
	}
	local := make(map[string]pathState, len(entries))
	for i, e := range entries {
		if (i == 0 || i%scanCancelCheckEvery == scanCancelCheckEvery-1) && w.stopped() {
			return
		}
		full := filepath.Join(path, e.Name())
		if fileutil.IsVCSStoreDir(e.Name()) {
			continue
		}
		if e.IsDir() {
			if w.stopped() {
				return
			}
			w.wg.Add(1)
			select {
			case w.jobs <- full:
			default:
				// A saturated pool must recurse inline to keep descendants moving.
				w.dir(full)
				w.wg.Done()
			}
			continue
		}
		if w.files.Add(1) > w.limit {
			w.over.Store(true)
			w.short.Store(true)
			return
		}
		info, err := e.Info()
		if err != nil {
			w.short.Store(true)
			continue
		}
		local[full] = pathState{exists: true, size: info.Size(), modTime: info.ModTime().UnixNano(), mode: info.Mode()}
	}
	w.mu.Lock()
	maps.Copy(w.state, local)
	w.mu.Unlock()
}
