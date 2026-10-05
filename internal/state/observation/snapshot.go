package observation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"reasonix/internal/state/trustedstate"
)

// Store keeps snapshot tree nodes. trustedstate.Store satisfies it. Nodes are
// an index, not a seal: a snapshot's authority is its digest in a record.
type Store interface {
	PutIndexObject([]byte) (trustedstate.Digest, error)
	Object(trustedstate.Digest) ([]byte, error)
}

// Snapshot is one observation of a workspace root. Digest covers every other
// field, so two snapshots agree only if they saw the same tree under the same
// policy and both saw all of it.
type Snapshot struct {
	Root       string `json:"root,omitempty"`
	Policy     string `json:"policy"`
	Entries    int    `json:"entries"`
	Complete   bool   `json:"complete"`
	Incomplete string `json:"incomplete,omitempty"`
	Digest     string `json:"digest"`
}

// Why a snapshot is incomplete.
const (
	IncompleteEntryLimit = "entry_limit"
	IncompleteWalkError  = "walk_error"
	IncompleteCancelled  = "cancelled"
	IncompleteStore      = "store"
)

type stamp struct {
	Size  int64  `json:"z"`
	Mtime int64  `json:"t"`
	Ctime int64  `json:"c,omitempty"`
	Ino   uint64 `json:"i,omitempty"`
	Dev   uint64 `json:"v,omitempty"`
}

// node is one directory entry. Kind is f (file), d (directory), l (symlink)
// or o (anything else); Ref is a subtree digest or a link target's digest.
type node struct {
	Name  string `json:"n"`
	Kind  string `json:"k"`
	Mode  uint32 `json:"m,omitempty"`
	Stamp *stamp `json:"s,omitempty"`
	Ref   string `json:"r,omitempty"`
}

// Observer takes snapshots under one policy and caches recently stored nodes
// to avoid repeated object-store work.
type Observer struct {
	store  Store
	policy Policy
	digest string

	mu    sync.Mutex
	known map[trustedstate.Digest]bool
}

// NewObserver returns an observer storing nodes in store under policy.
func NewObserver(store Store, policy Policy) *Observer {
	return &Observer{store: store, policy: policy, digest: policy.Digest(), known: map[trustedstate.Digest]bool{}}
}

// readers bounds live walk workers, including metadata reads and object writes.
const readers = 16

type walk struct {
	o       *Observer
	ctx     context.Context
	entries atomic.Int64
	jobs    chan func()
	mu      sync.Mutex
	reason  string
}

func (w *walk) fail(reason string) {
	w.mu.Lock()
	if w.reason == "" {
		w.reason = reason
	}
	w.mu.Unlock()
}

func (w *walk) failed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reason != ""
}

// Take observes root. A snapshot that fell short says why and is never
// Complete; it still carries a digest so the shortfall itself is recorded.
func (o *Observer) Take(ctx context.Context, root string) Snapshot {
	w := &walk{o: o, ctx: ctx, jobs: make(chan func())}
	var workers sync.WaitGroup
	for range readers - 1 {
		workers.Go(func() {
			for job := range w.jobs {
				job()
			}
		})
	}
	ref := w.dir(root)
	close(w.jobs)
	workers.Wait()
	if ctx.Err() != nil {
		w.fail(IncompleteCancelled)
	}
	s := Snapshot{Root: ref, Policy: o.digest, Entries: int(w.entries.Load()), Complete: !w.failed()}
	s.Incomplete = w.reason
	s.Digest = s.digestOf()
	return s
}

func (s Snapshot) digestOf() string {
	s.Digest = ""
	b, _ := json.Marshal(s)
	return string(trustedstate.DigestOf(b))
}

func (w *walk) dir(path string) string {
	if w.failed() || w.ctx.Err() != nil {
		return ""
	}
	nodes, subdirs := w.readNodes(path)
	var wg sync.WaitGroup
	for _, i := range subdirs {
		if w.failed() || w.ctx.Err() != nil {
			break
		}
		wg.Add(1)
		job := func() {
			defer wg.Done()
			nodes[i].Ref = w.dir(filepath.Join(path, nodes[i].Name))
		}
		select {
		case w.jobs <- job:
		default:
			// Busy workers may be waiting on descendants, so dispatch cannot block.
			job()
		}
	}
	wg.Wait()
	if w.failed() || w.ctx.Err() != nil {
		return ""
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return w.o.put(w, nodes)
}

func (w *walk) readNodes(path string) ([]node, []int) {
	nodes := make([]node, 0)
	var subdirs []int
	f, err := os.Open(path)
	if err != nil {
		w.fail(IncompleteWalkError)
		return nodes, subdirs
	}
	defer f.Close()
	for {
		if w.failed() || w.ctx.Err() != nil {
			return nodes, subdirs
		}
		entries, err := f.ReadDir(256)
		for _, e := range entries {
			if w.failed() || w.ctx.Err() != nil {
				return nodes, subdirs
			}
			if w.entries.Add(1) > int64(w.o.policy.MaxEntries) {
				w.fail(IncompleteEntryLimit)
				return nodes, subdirs
			}
			info, ierr := e.Info()
			if ierr != nil {
				w.fail(IncompleteWalkError)
				continue
			}
			n := node{Name: e.Name(), Mode: uint32(info.Mode().Perm())}
			switch mode := info.Mode(); {
			case mode.IsDir():
				if w.o.policy.excludes(e.Name(), filepath.Clean(filepath.Join(path, e.Name()))) {
					w.entries.Add(-1)
					continue
				}
				n.Kind = "d"
				subdirs = append(subdirs, len(nodes))
			case mode&os.ModeSymlink != 0:
				n.Kind = "l"
				target, lerr := os.Readlink(filepath.Join(path, e.Name()))
				if lerr != nil {
					w.fail(IncompleteWalkError)
				}
				n.Ref = string(trustedstate.DigestOf([]byte(target)))
			case mode.IsRegular():
				n.Kind = "f"
				st := stampOf(info)
				n.Stamp = &st
			default:
				n.Kind = "o"
				n.Mode = uint32(mode)
			}
			nodes = append(nodes, n)
		}
		if errors.Is(err, io.EOF) {
			return nodes, subdirs
		}
		if err != nil {
			w.fail(IncompleteWalkError)
			return nodes, subdirs
		}
	}
}

func (o *Observer) put(w *walk, nodes []node) string {
	if w.failed() || w.ctx.Err() != nil {
		return ""
	}
	b, err := json.Marshal(nodes)
	if err != nil {
		w.fail(IncompleteStore)
		return ""
	}
	d := trustedstate.DigestOf(b)
	o.mu.Lock()
	seen := o.known[d]
	o.mu.Unlock()
	if seen {
		return string(d)
	}
	if _, err := o.store.PutIndexObject(b); err != nil {
		w.fail(IncompleteStore)
		return string(d)
	}
	o.mu.Lock()
	if len(o.known) >= max(o.policy.MaxEntries, 1) {
		clear(o.known)
	}
	o.known[d] = true
	o.mu.Unlock()
	return string(d)
}

// ErrIncomparable: at least one snapshot is incomplete, or the two were taken
// under different policies, so no difference between them is established.
var ErrIncomparable = errors.New("snapshots are not comparable")
