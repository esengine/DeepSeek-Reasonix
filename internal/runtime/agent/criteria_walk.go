package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/safety/evidence"
)

// criteriaReaders bounds the directory and file reads a criteria capture keeps
// in flight. A workspace on a network drive answers each read in a round trip,
// so a walk that asks one question at a time is bound by latency, not by the
// number of files.
const criteriaReaders = 16

// criteriaWalk lists every path under root that could hold a test criterion,
// reading directories concurrently. The name settles the question, so no file
// is opened here.
type criteriaWalk struct {
	ctx     context.Context
	readDir func(string) ([]os.DirEntry, error)
	mu      sync.Mutex
	wake    *sync.Cond
	queue   []string
	active  int
	found   []string
}

// criterionCandidates drains a queue of directories with a fixed set of readers.
// Directories wait in the queue rather than being handed to a reader that may
// not be listening yet, so every reader is working as soon as there is more than
// one directory to read.
func criterionCandidates(ctx context.Context, root string, readDir func(string) ([]os.DirEntry, error)) []string {
	w := &criteriaWalk{ctx: ctx, readDir: readDir, queue: []string{root}}
	w.wake = sync.NewCond(&w.mu)
	var readers sync.WaitGroup
	for range criteriaReaders {
		readers.Go(w.read)
	}
	readers.Wait()
	slices.Sort(w.found)
	return w.found
}

func (w *criteriaWalk) read() {
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && w.active > 0 {
			w.wake.Wait()
		}
		if len(w.queue) == 0 {
			w.mu.Unlock()
			w.wake.Broadcast()
			return
		}
		dir := w.queue[len(w.queue)-1]
		w.queue = w.queue[:len(w.queue)-1]
		w.active++
		w.mu.Unlock()

		files, dirs := w.list(dir)

		w.mu.Lock()
		w.found = append(w.found, files...)
		w.queue = append(w.queue, dirs...)
		w.active--
		w.mu.Unlock()
		w.wake.Broadcast()
	}
}

// list reads one directory and sorts its entries into candidate files and the
// directories still to read.
func (w *criteriaWalk) list(path string) (files, dirs []string) {
	if w.ctx.Err() != nil {
		return nil, nil
	}
	entries, err := w.readDir(path)
	if err != nil {
		return nil, nil
	}
	for i, e := range entries {
		if i%criteriaWalkCancelCheckEvery == criteriaWalkCancelCheckEvery-1 && w.ctx.Err() != nil {
			return files, nil
		}
		full := filepath.Join(path, e.Name())
		if e.IsDir() {
			if !fileutil.IsVCSStoreDir(e.Name()) {
				dirs = append(dirs, full)
			}
			continue
		}
		if evidence.PathMayHoldTestCriteria(full) {
			files = append(files, full)
		}
	}
	return files, dirs
}

// criterionBytes reads the candidates concurrently and keeps the ones that hold
// criteria. A file that cannot be read holds none, as it did when each was read
// in turn.
func criterionBytes(ctx context.Context, paths []string) map[string][]byte {
	held := make(map[string][]byte)
	var mu sync.Mutex
	slots := make(chan struct{}, criteriaReaders)
	var wg sync.WaitGroup
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			content, err := os.ReadFile(path)
			if err != nil || !evidence.HoldsTestCriteria(path, content) {
				return
			}
			mu.Lock()
			held[path] = content
			mu.Unlock()
		})
	}
	wg.Wait()
	return held
}
