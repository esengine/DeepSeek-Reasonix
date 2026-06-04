// Package checkpoint is reasonix's snapshot-based edit safety net. Before a writer
// tool changes a file, the agent records the file's pre-edit content here, keyed
// to the current user turn; a frontend can then rewind the workspace (and, via the
// controller, the conversation) to an earlier turn.
//
// It is deliberately git-free (like Claude Code's rewind): snapshots live beside
// the session, never touch the user's git, and work in a non-git directory. Only
// edit-tool changes are tracked — bash side effects are not (a shell command's
// targets can't be known in advance), which is why the capture hook only fires for
// tools that can Preview their change.
package checkpoint

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/diff"
	fileenc "reasonix/internal/fileutil/encoding"
)

// FileSnap is one file's state at the moment it was first touched in a turn.
// Content == nil means the file did not exist then, so a restore deletes it.
type FileSnap struct {
	Path     string        `json:"path"`
	Content  *string       `json:"content"`
	Encoding *fileenc.Kind `json:"encoding,omitempty"`
}

// Checkpoint anchors the pre-edit state of every distinct file touched during one
// user turn. MsgIndex is len(Session.Messages) at the turn's start — the
// conversation-rewind boundary — persisted so a resumed session can rewind the
// conversation and fork, not just the code.
type Checkpoint struct {
	Turn     int        `json:"turn"`
	Time     time.Time  `json:"time"`
	Prompt   string     `json:"prompt"`
	MsgIndex int        `json:"msgIndex"`
	Files    []FileSnap `json:"files"`
}

// Meta is the picker-facing summary of a checkpoint (no file contents).
type Meta struct {
	Turn   int
	Time   time.Time
	Prompt string
	Paths  []string
}

// Store holds a session's checkpoints in memory and, when dir is set, persists one
// JSON file per turn under it (cheap delete, corruption-isolated). All methods are
// safe for concurrent use — the agent snapshots from tool goroutines.
type Store struct {
	dir  string // <session>.ckpt/, or "" for in-memory only
	root string // workspace root, for restore path-escape guards

	mu   sync.Mutex
	done []*Checkpoint   // finalized turns
	cur  *Checkpoint     // the active turn's checkpoint
	seen map[string]bool // paths already snapshotted this turn (dedup)
}

// New returns a store for the given checkpoint dir and workspace root, loading any
// checkpoints already persisted under dir. A "" dir disables persistence (the
// store still works in memory for the session).
func New(dir, root string) *Store {
	s := &Store{dir: dir, root: root, seen: map[string]bool{}}
	if dir != "" {
		s.load()
	}
	return s
}

func (s *Store) load() {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var c Checkpoint
		if json.Unmarshal(b, &c) == nil {
			s.done = append(s.done, &c)
		}
	}
	sort.Slice(s.done, func(i, j int) bool { return s.done[i].Turn < s.done[j].Turn })
}

// Begin opens a checkpoint for a new user turn, finalizing the previous one. The
// prompt labels it in the picker; msgIndex is the conversation-rewind boundary.
func (s *Store) Begin(turn int, prompt string, msgIndex int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil {
		s.done = append(s.done, s.cur)
	}
	s.cur = &Checkpoint{Turn: turn, Time: time.Now(), Prompt: prompt, MsgIndex: msgIndex}
	s.seen = map[string]bool{}
	s.persist(s.cur)
}

// Bounds returns turn → MsgIndex over all checkpoints (persisted + current), so
// the controller can rebuild its conversation-rewind boundaries after loading a
// resumed session's checkpoints from disk.
func (s *Store) Bounds() map[int]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := make(map[int]int, len(s.done)+1)
	for _, c := range s.done {
		m[c.Turn] = c.MsgIndex
	}
	if s.cur != nil {
		m[s.cur.Turn] = s.cur.MsgIndex
	}
	return m
}

// Snapshot records the pre-edit state of the file a writer is about to change.
// Only the first touch of a path in the current turn is kept (that is its
// turn-start content). A no-op before the first Begin.
func (s *Store) Snapshot(ch diff.Change) {
	if ch.Path == "" {
		return
	}
	var enc *fileenc.Kind
	if ch.Kind != diff.Create {
		enc = s.detectEncoding(ch.Path)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil || s.seen[ch.Path] {
		return
	}
	s.seen[ch.Path] = true
	var content *string
	if ch.Kind != diff.Create { // create == file didn't exist → leave nil (restore deletes)
		old := ch.OldText
		content = &old
	}
	s.cur.Files = append(s.cur.Files, FileSnap{Path: ch.Path, Content: content, Encoding: enc})
	s.persist(s.cur)
}

func (s *Store) detectEncoding(p string) *fileenc.Kind {
	abs, err := safePath(s.root, p)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	enc, _ := fileenc.Detect(b)
	return &enc
}

func (s *Store) persist(c *Checkpoint) {
	if s.dir == "" {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		slog.Warn("checkpoint: create dir failed", "dir", s.dir, "err", err)
		return
	}
	if err := os.WriteFile(filepath.Join(s.dir, fmt.Sprintf("turn-%d.json", c.Turn)), b, 0o644); err != nil {
		slog.Warn("checkpoint: persist failed", "turn", c.Turn, "err", err)
	}
}

// NextTurn returns the turn number a new checkpoint should take: one past the
// highest existing turn (0 when empty), so a resumed session keeps numbering
// without colliding with checkpoints loaded from disk.
func (s *Store) NextTurn() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := 0
	for _, c := range s.done {
		if c.Turn >= next {
			next = c.Turn + 1
		}
	}
	if s.cur != nil && s.cur.Turn >= next {
		next = s.cur.Turn + 1
	}
	return next
}

// List returns every checkpoint's metadata, oldest turn first.
func (s *Store) List() []Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Meta, 0, len(s.done)+1)
	for _, c := range s.all() {
		paths := make([]string, len(c.Files))
		for i, f := range c.Files {
			paths[i] = f.Path
		}
		out = append(out, Meta{Turn: c.Turn, Time: c.Time, Prompt: c.Prompt, Paths: paths})
	}
	return out
}

// all returns done + cur in turn order. Caller holds the lock.
func (s *Store) all() []*Checkpoint {
	cps := append([]*Checkpoint(nil), s.done...)
	if s.cur != nil {
		cps = append(cps, s.cur)
	}
	sort.Slice(cps, func(i, j int) bool { return cps[i].Turn < cps[j].Turn })
	return cps
}

// RestoreCode reverts the workspace to its state at the start of turn `fromTurn`:
// for every file touched in turn fromTurn or later, it writes back that file's
// earliest recorded content (or deletes it when the earliest snapshot was nil).
// Returns the paths written and deleted.
func (s *Store) RestoreCode(fromTurn int) (written, deleted []string, err error) {
	s.mu.Lock()
	// earliest snapshot per path across checkpoints >= fromTurn (turn order → first wins).
	earliest := map[string]FileSnap{}
	order := []string{}
	for _, c := range s.all() {
		if c.Turn < fromTurn {
			continue
		}
		for _, f := range c.Files {
			if _, ok := earliest[f.Path]; ok {
				continue
			}
			earliest[f.Path] = f
			order = append(order, f.Path)
		}
	}
	root := s.root
	s.mu.Unlock()

	for _, p := range order {
		abs, gerr := safePath(root, p)
		if gerr != nil {
			err = gerr
			continue
		}
		snap := earliest[p]
		if snap.Content == nil {
			if rmErr := os.Remove(abs); rmErr == nil {
				deleted = append(deleted, p)
			} else if !os.IsNotExist(rmErr) {
				err = rmErr
			}
			continue
		}
		if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
			err = mkErr
			continue
		}
		enc := fileenc.UTF8
		if snap.Encoding != nil {
			enc = *snap.Encoding
		} else if current := detectCurrentEncoding(abs); current != nil {
			enc = *current
		}
		if wErr := os.WriteFile(abs, fileenc.Encode(*snap.Content, enc), 0o644); wErr != nil {
			err = wErr
			continue
		}
		written = append(written, p)
	}
	return written, deleted, err
}

func detectCurrentEncoding(path string) *fileenc.Kind {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	enc, _ := fileenc.Detect(b)
	return &enc
}

// safePath resolves p against root and rejects anything escaping it — restore
// must never write outside the workspace, even if a snapshot path is hostile or
// the project moved since it was taken.
func safePath(root, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, p)
	}
	abs = filepath.Clean(abs)
	if root != "" {
		r := filepath.Clean(root)
		if abs != r && !strings.HasPrefix(abs, r+string(os.PathSeparator)) {
			return "", fmt.Errorf("checkpoint path %q escapes workspace %q", p, root)
		}
	}
	return abs, nil
}
