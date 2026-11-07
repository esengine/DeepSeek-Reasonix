// move.go — relocating one root: copy, verify, commit, reclaim.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/config"
)

// Phase is where a move got to. The order is the guarantee: everything before
// Committed can be abandoned and leaves the runtime on its original data;
// everything from Committed on can only be retried, because the configuration
// already points at the copy.
type Phase string

const (
	PhaseCopying   Phase = "copying"
	PhaseVerifying Phase = "verifying"
	PhaseCommitted Phase = "committed"
	PhaseDone      Phase = "done"
	// PhaseAdopting is the whole of a move whose target already holds the data.
	PhaseAdopting Phase = "adopting"
)

// Journal is the record a move leaves behind so an interrupted one can be
// finished rather than guessed at. It lives in the home root, the one root a
// move never touches.
type Journal struct {
	Root    config.RootID `json:"root"`
	From    string        `json:"from"`
	To      string        `json:"to"`
	Phase   Phase         `json:"phase"`
	Started time.Time     `json:"started"`
	Updated time.Time     `json:"updated"`
}

// Progress reports a move as it runs. Bytes copied against bytes planned is
// what a person watches; Phase is what tells them whether cancelling is still
// free.
type Progress struct {
	Phase  Phase
	Bytes  int64
	Total  int64
	Files  int64
	Detail string
}

// ErrMoveInterrupted means the caller cancelled before the commit. Nothing was
// changed: the runtime still reads the original location.
var ErrMoveInterrupted = errors.New("storage: move interrupted before commit")

// ErrSourceChanging means the source kept changing faster than the copy could
// catch up. Nothing was committed, so the runtime is still on its own data.
var ErrSourceChanging = errors.New("storage: the folder being moved keeps changing while it is copied")

// maxCatchUpRounds bounds how many times a move re-copies what a running
// process wrote after the first pass before it gives up.
const maxCatchUpRounds = 3

// Move relocates a root: copy, prove the copy, then point the configuration at
// it — a failure before that last step costs only bytes the next attempt reuses.
// The running process keeps reading the old location, because open catalogs and
// session leases hold handles that cannot be moved underneath them; the new one
// takes effect on the next launch.
func Move(ctx context.Context, plan Plan, report func(Progress)) error {
	if !plan.OK() {
		return fmt.Errorf("storage: move refused: %s", plan.Refusals[0].Detail)
	}
	notify := func(p Progress) {
		if report != nil {
			report(p)
		}
	}
	if plan.Adopt {
		return adopt(plan, notify)
	}
	journal := Journal{Root: plan.Root, From: plan.From, To: plan.To, Phase: PhaseCopying, Started: time.Now()}
	if err := writeJournal(journal); err != nil {
		return err
	}
	if err := os.MkdirAll(plan.To, 0o755); err != nil {
		return err
	}

	owned := presentEntries(plan.From, movedEntries(plan.Root))
	copied, err := copyOwned(ctx, owned, plan, notify)
	if err != nil {
		return err
	}

	journal.Phase = PhaseVerifying
	notify(Progress{Phase: PhaseVerifying, Bytes: copied, Total: plan.Bytes})
	if err := writeJournal(journal); err != nil {
		return err
	}
	seen, err := settleCopy(ctx, owned, plan, notify)
	if err != nil {
		return err
	}

	// Recorded once the copy is proved and before the configuration is: a
	// marker means this folder is complete, which is what lets a later run
	// point at it when the configuration naming it is gone.
	if err := WriteMarker(plan.To, plan.Root, plan.From); err != nil {
		return fmt.Errorf("storage: %s cannot record what it holds: %w", plan.To, err)
	}

	// The point of no return, and the only write that changes what the next
	// launch reads. Everything above this line is a copy nobody depends on.
	journal.Phase = PhaseCommitted
	if err := writeJournal(journal); err != nil {
		return err
	}
	if err := commitLocation(plan.Root, plan.To); err != nil {
		return fmt.Errorf("storage: copy verified but the location could not be recorded: %w", err)
	}
	notify(Progress{Phase: PhaseCommitted, Bytes: copied, Total: plan.Bytes})

	// Reclaiming is the one step allowed to fail quietly: the move already
	// succeeded, and a source that could not be deleted is wasted space rather
	// than lost data.
	if changed := changedSince(plan.From, owned, seen); changed != "" {
		notify(Progress{Phase: PhaseDone, Bytes: copied, Total: plan.Bytes,
			Detail: "moved; the old folder was written to after the copy was checked, so it was kept instead of deleted: " + changed})
		journal.Phase = PhaseDone
		_ = writeJournal(journal)
		return nil
	}
	if err := reclaimOwned(plan.From, owned); err != nil {
		notify(Progress{Phase: PhaseDone, Bytes: copied, Total: plan.Bytes,
			Detail: "moved; the old folder could not be removed and can be deleted by hand"})
		journal.Phase = PhaseDone
		_ = writeJournal(journal)
		return nil
	}
	journal.Phase = PhaseDone
	_ = writeJournal(journal)
	notify(Progress{Phase: PhaseDone, Bytes: copied, Total: plan.Bytes})
	return clearJournal()
}

// adopt points a root at data already sitting in the target. There is nothing
// to copy, verify or reclaim, and so nothing to journal either: an interrupted
// adopt has changed nothing, and the single write it does is the write an
// ordinary move ends with. What the old location still holds stays there —
// deleting it would be the one destructive act this operation does not need.
func adopt(plan Plan, notify func(Progress)) error {
	notify(Progress{Phase: PhaseAdopting, Total: plan.Bytes})
	if err := WriteMarker(plan.To, plan.Root, plan.From); err != nil {
		return fmt.Errorf("storage: %s cannot record what it holds: %w", plan.To, err)
	}
	if err := commitLocation(plan.Root, plan.To); err != nil {
		return fmt.Errorf("storage: the location could not be recorded: %w", err)
	}
	notify(Progress{Phase: PhaseDone, Bytes: plan.Bytes, Total: plan.Bytes})
	return nil
}

// movedEntries is what this move may touch: the root's declared entries, or a
// single empty name meaning the directory itself when the root has it alone.
func movedEntries(id config.RootID) []string {
	if owned := config.RootOwns(id); len(owned) > 0 {
		return owned
	}
	return []string{""}
}

// presentEntries keeps the ones that actually exist, so every later step works
// on one list: a root that never grew a stats folder has nothing to copy,
// verify, or reclaim there.
func presentEntries(from string, entries []string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(from, entry)); err == nil {
			out = append(out, entry)
		}
	}
	return out
}

// copyOwned runs the copy over each entry the root owns, reporting one running
// total across them so progress reads as a single operation.
func copyOwned(ctx context.Context, owned []string, plan Plan, notify func(Progress)) (int64, error) {
	var copied int64
	for _, entry := range owned {
		from := filepath.Join(plan.From, entry)
		n, err := copyTree(ctx, from, filepath.Join(plan.To, entry), plan.Bytes, copied, notify)
		copied += n
		if err != nil {
			return copied, err
		}
	}
	return copied, nil
}

// reclaimOwned removes only what was copied. A root sharing its directory
// leaves the neighbour's files exactly where they were. The marker goes with
// them: a location this root has left must stop claiming to hold it.
func reclaimOwned(from string, owned []string) error {
	for _, entry := range owned {
		if err := os.RemoveAll(filepath.Join(from, entry)); err != nil {
			return err
		}
	}
	if err := fileutil.Remove(filepath.Join(from, markerName)); err != nil {
		return err
	}
	return nil
}

// copyTree mirrors from into to. A file already present at the right size is
// left alone, which is what makes a second run finish an interrupted first one
// instead of starting over.
func copyTree(ctx context.Context, from, to string, total, already int64, notify func(Progress)) (int64, error) {
	var copied int64
	var files int64
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return errors.Join(ErrMoveInterrupted, ctxErr)
		}
		rel, relErr := filepath.Rel(from, path)
		if relErr != nil {
			return relErr
		}
		// A marker names a location, so it never travels: one at the target
		// has to mean the copy that landed there finished.
		if rel == markerName {
			return nil
		}
		dest := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if existing, statErr := os.Stat(dest); statErr == nil && existing.Size() == info.Size() {
			copied += info.Size()
			files++
			return nil
		}
		if err := copyFile(path, dest, info); err != nil {
			return err
		}
		copied += info.Size()
		files++
		notify(Progress{Phase: PhaseCopying, Bytes: already + copied, Total: total, Files: files})
		return nil
	})
	return copied, err
}

func copyFile(from, to string, info fs.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	// Written beside the target and renamed into place, so an interrupted copy
	// never leaves a short file the size check above would accept as done.
	tmp, err := os.CreateTemp(filepath.Dir(to), ".reasonix-move-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := fileutil.ReplaceFile(tmpName, to); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// seenFile is what a source file looked like when its copy was proved.
type seenFile struct {
	size int64
	mod  time.Time
}

// settleCopy proves every source file arrived byte for byte, copying again what
// a still-running process wrote after the first pass. Size alone would accept a
// torn copy that happened to land on the same length, and this is the last
// check before the source is deleted. It reports what it saw so the caller can
// tell, after the commit, whether the source moved again.
func settleCopy(ctx context.Context, owned []string, plan Plan, notify func(Progress)) (map[string]seenFile, error) {
	for round := 0; ; round++ {
		seen := map[string]seenFile{}
		var stale []string
		var verified, files int64
		for _, entry := range owned {
			from, to := filepath.Join(plan.From, entry), filepath.Join(plan.To, entry)
			err := walkFiles(ctx, from, func(rel string, info fs.FileInfo) error {
				src := filepath.Join(from, rel)
				same, err := sameBytes(src, filepath.Join(to, rel))
				if err != nil {
					return err
				}
				verified += info.Size()
				files++
				notify(Progress{Phase: PhaseVerifying, Bytes: verified, Total: plan.Bytes, Files: files})
				if same {
					seen[src] = seenFile{size: info.Size(), mod: info.ModTime()}
					return nil
				}
				stale = append(stale, filepath.Join(entry, rel))
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		if len(stale) == 0 {
			return seen, nil
		}
		if round == maxCatchUpRounds {
			return nil, fmt.Errorf("%w: %d files still differ (first: %s)", ErrSourceChanging, len(stale), stale[0])
		}
		notify(Progress{Phase: PhaseVerifying, Bytes: verified, Total: plan.Bytes, Files: files,
			Detail: fmt.Sprintf("%d files changed while they were being copied; copying them again", len(stale))})
		for _, rel := range stale {
			src := filepath.Join(plan.From, rel)
			info, err := os.Stat(src)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := copyFile(src, filepath.Join(plan.To, rel), info); err != nil {
				return nil, err
			}
		}
	}
}

// walkFiles visits every regular file under root except a marker, handing the
// callback its path relative to root. A file that vanishes between listing and
// stat was deleted by the running process, which is not a copy failure.
func walkFiles(ctx context.Context, root string, visit func(rel string, info fs.FileInfo) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return errors.Join(ErrMoveInterrupted, ctxErr)
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == markerName {
			return nil
		}
		info, infoErr := entry.Info()
		if errors.Is(infoErr, fs.ErrNotExist) {
			return nil
		}
		if infoErr != nil {
			return infoErr
		}
		return visit(rel, info)
	})
}

// sameBytes reports whether the file at dst holds exactly what src holds. A
// destination that is not there yet is a difference to copy, not an error; a
// source that is gone is treated as equal because there is nothing left to prove.
func sameBytes(src, dst string) (bool, error) {
	srcSum, err := fileDigest(src)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	dstSum, err := fileDigest(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("verify %s: %w", dst, err)
	}
	return srcSum == dstSum, nil
}

// changedSince names the first file under owned that is new or differs from
// what the proof saw, or "" when the source stood still. The runtime keeps
// writing to the old location until it restarts, so anything it wrote after the
// proof exists nowhere else and must not be deleted with the rest.
func changedSince(from string, owned []string, seen map[string]seenFile) string {
	var changed string
	for _, entry := range owned {
		root := filepath.Join(from, entry)
		_ = walkFiles(context.Background(), root, func(rel string, info fs.FileInfo) error {
			path := filepath.Join(root, rel)
			if was, ok := seen[path]; !ok || was.size != info.Size() || !was.mod.Equal(info.ModTime()) {
				changed = filepath.Join(entry, rel)
				return fs.SkipAll
			}
			return nil
		})
		if changed != "" {
			return changed
		}
	}
	return ""
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// commitLocation records the new location through the ordinary config editor,
// so a relocation is an edit like any other and survives later rewrites.
func commitLocation(root config.RootID, dir string) error {
	path := config.UserConfigPath()
	if path == "" {
		return errors.New("no user config to record the location in")
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	edit := config.LoadForEdit(path)
	if err := edit.SetStorageDir(root, dir); err != nil {
		return err
	}
	if err := edit.SaveTo(path); err != nil {
		return err
	}
	config.InvalidateStorageDirs()
	return nil
}

// journalPath is in the home root on purpose: it has to survive the move of
// every root that can move, and home is the one that cannot.
func journalPath() string {
	home := config.RootDir(config.RootHome)
	if home == "" {
		return ""
	}
	return filepath.Join(home, "storage-move.json")
}

func writeJournal(j Journal) error {
	path := journalPath()
	if path == "" {
		return errors.New("storage: nowhere to record the move")
	}
	j.Updated = time.Now()
	body, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, append(body, '\n'), 0o600)
}

func clearJournal() error {
	path := journalPath()
	if path == "" {
		return nil
	}
	if err := fileutil.Remove(path); err != nil {
		return err
	}
	return nil
}

// PendingMove reports a move that did not reach done, so a launch can finish or
// report it instead of leaving the user with two half-populated folders and no
// account of which one is live.
func PendingMove() (Journal, bool) {
	path := journalPath()
	if path == "" {
		return Journal{}, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Journal{}, false
	}
	var j Journal
	if err := json.Unmarshal(body, &j); err != nil || j.Phase == "" || j.Phase == PhaseDone {
		return Journal{}, false
	}
	return j, true
}
