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
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"reasonix/internal/diff"
	"reasonix/internal/fileutil"
	fileenc "reasonix/internal/fileutil/encoding"
)

// FileSnap is one file's state at the moment it was first touched in a turn.
// Content == nil means the file did not exist then, so a restore deletes it.
// DestPath, when set, marks a move-back pointer: the rename moved Path→DestPath
// and could not be content-captured, so restore reverses it by moving DestPath
// back to Path. Renames whose source content was readable are stored as ordinary
// content snapshots instead and never set DestPath.
type FileSnap struct {
	Path     string        `json:"path"`
	Content  *string       `json:"content"`
	Encoding *fileenc.Kind `json:"encoding,omitempty"`
	DestPath string        `json:"dest_path,omitempty"`
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
		b, err := fileenc.ReadFileUTF8(filepath.Join(s.dir, e.Name()))
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
//
// Rename is special: Preview carries no OldText, so Snapshot reads the source
// and destination from disk and records both paths' actual turn-start state.
// Content-based restore remains correct if the destination is later edited,
// deleted, or renamed again in the same or a later turn.
func (s *Store) Snapshot(ch diff.Change) {
	if ch.Path == "" {
		return
	}

	if ch.Kind == diff.Rename {
		s.snapshotRename(ch)
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

// snapshotRename captures enough turn-start state to reverse a move. When the
// source content is readable it records both paths as ordinary content
// snapshots (src's turn-start content, dst's turn-start state) — content-based
// restore stays correct even if dst is later edited, deleted, or renamed again.
// When the source cannot be content-captured (unreadable, or resolving outside
// the checkpoint root) it falls back to a move-back pointer: a FileSnap whose
// DestPath tells RestoreCode to move dst back to src with a rename, so a
// transient read error never silently forfeits reversibility.
func (s *Store) snapshotRename(ch diff.Change) {
	src, srcOK := s.snapshotDiskPath(ch.Path)
	dst, dstOK := s.snapshotDiskPath(ch.DestPath)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil {
		return
	}

	if srcOK {
		// Content path: record src's turn-start content and dst's turn-start
		// state (nil when absent → restore deletes the moved copy), each deduped
		// independently on first touch. Independent dedup is what preserves a
		// chained rename (a→b→c): the second rename's src (b) was already the
		// first's dst, so it is skipped, but its dst (c) is still recorded as nil
		// so restore removes it.
		if !s.seen[ch.Path] {
			s.seen[ch.Path] = true
			s.cur.Files = append(s.cur.Files, src)
		}
		if dstOK && ch.DestPath != "" && !s.seen[ch.DestPath] {
			s.seen[ch.DestPath] = true
			s.cur.Files = append(s.cur.Files, dst)
		}
		s.persist(s.cur)
		return
	}

	// Fallback: source content is unknown (unreadable, or resolving outside the
	// checkpoint root). Record a move-back pointer instead of a content snapshot
	// so a transient read error never silently forfeits reversibility.
	// Deliberately no paired nil-dst snapshot — that could delete the moved file
	// before the reversal runs. If src resolves outside the checkpoint root,
	// RestoreCode's safePath rejects the pointer and leaves dst intact rather
	// than destroying the only copy.
	if ch.DestPath == "" || s.seen[ch.Path] {
		return
	}
	s.seen[ch.Path] = true
	s.cur.Files = append(s.cur.Files, FileSnap{Path: ch.Path, DestPath: ch.DestPath})
	s.persist(s.cur)
}

// snapshotDiskPath captures a path exactly as it exists immediately before a
// rename. A missing path is a valid nil-content snapshot; other read failures
// are not recorded because treating them as missing would make rewind delete a
// file whose original state was unknown.
func (s *Store) snapshotDiskPath(path string) (FileSnap, bool) {
	snap := FileSnap{Path: path}
	if path == "" {
		return snap, false
	}
	abs, err := safePath(s.root, path)
	if err != nil {
		return snap, false
	}
	b, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		return snap, true
	}
	if err != nil {
		return snap, false
	}
	enc, raw := fileenc.Detect(b)
	content := string(fileenc.Decode(raw, enc))
	snap.Content = &content
	snap.Encoding = &enc
	return snap, true
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

// TruncateFrom discards checkpoints at or after fromTurn. Conversation rewind
// removes those future turns from the transcript, so their file snapshots must
// not remain visible or collide with newly-created checkpoints that reuse the
// same turn numbers after the rewrite.
func (s *Store) TruncateFrom(fromTurn int) {
	s.mu.Lock()
	done := s.done[:0]
	deleteTurns := map[int]bool{}
	for _, c := range s.done {
		if c.Turn >= fromTurn {
			deleteTurns[c.Turn] = true
			continue
		}
		done = append(done, c)
	}
	for i := len(done); i < len(s.done); i++ {
		s.done[i] = nil
	}
	s.done = done
	if s.cur != nil && s.cur.Turn >= fromTurn {
		deleteTurns[s.cur.Turn] = true
		s.cur = nil
		s.seen = map[string]bool{}
	}
	dir := s.dir
	s.mu.Unlock()

	if dir == "" || len(deleteTurns) == 0 {
		return
	}
	for turn := range deleteTurns {
		if err := os.Remove(filepath.Join(dir, fmt.Sprintf("turn-%d.json", turn))); err != nil && !os.IsNotExist(err) {
			slog.Warn("checkpoint: truncate failed", "turn", turn, "err", err)
		}
	}
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

	protected := map[string]bool{}
	for _, p := range order {
		if protected[p] {
			continue
		}
		snap := earliest[p]
		// DestPath marks a move-back pointer: the rename could not be content-
		// captured, so reverse it by moving DestPath back to Path. Content-captured
		// renames are restored by the ordinary content branch below and never
		// set DestPath.
		if snap.DestPath != "" {
			dstAbs, gerr := safePath(root, snap.DestPath)
			if gerr != nil {
				err = gerr
				protected[snap.DestPath] = true
				continue
			}
			srcAbs, gerr := safePath(root, snap.Path)
			if gerr != nil {
				err = gerr
				protected[snap.DestPath] = true
				continue
			}
			if _, statErr := os.Stat(dstAbs); statErr == nil {
				if moveErr := moveForRestore(dstAbs, srcAbs); moveErr == nil {
					written = append(written, snap.Path)
					deleted = append(deleted, snap.DestPath)
				} else {
					err = moveErr
					// The destination may be the only remaining copy. Do not let
					// its paired nil snapshot delete it after a failed reversal.
					protected[snap.DestPath] = true
				}
			} else if !os.IsNotExist(statErr) {
				err = statErr
				protected[snap.DestPath] = true
			}
			continue
		}
		abs, gerr := safePath(root, p)
		if gerr != nil {
			err = gerr
			continue
		}
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

var renameForRestore = os.Rename

// moveForRestore mirrors move_file's cross-filesystem fallback for legacy
// rename checkpoints. The source is removed only after a complete destination
// copy exists, so failures retain at least one intact copy.
func moveForRestore(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := renameForRestore(src, dst); err == nil {
		return nil
	} else if !fileutil.IsCrossDeviceError(err) {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	inClosed := false
	closeIn := func() {
		if !inClosed {
			inClosed = true
			_ = in.Close()
		}
	}
	defer closeIn()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("checkpoint restore source %q is not a regular file", src)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("checkpoint restore destination %q already exists", dst)
	} else if !os.IsNotExist(err) {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".reasonix-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = io.Copy(tmp, in); err == nil {
		err = tmp.Chmod(info.Mode().Perm())
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	closeIn()
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, dst); err != nil {
		return err
	}
	keepTemp = true // tmpName is now dst; never remove the completed copy.
	if err = os.Remove(src); err != nil {
		return err
	}
	return nil
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
		rel, err := filepath.Rel(r, abs)
		if err != nil || !filepath.IsLocal(rel) {
			return "", fmt.Errorf("checkpoint path %q escapes workspace %q", p, root)
		}
	}
	return abs, nil
}
