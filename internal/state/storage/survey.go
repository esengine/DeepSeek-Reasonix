// Package storage measures what the runtime keeps on disk: how much each
// declared root holds, and what the volume under it has left. It reads the root
// table rather than a list of its own, so a root declared in internal/contract/config is
// accounted for here without this package being told about it.
package storage

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/contract/config"
)

// Root is one root as measured: where it resolved to, whether a user may move
// it, and what it costs. Bytes and Files are what the walk actually counted —
// a surface that offers to delete things reports measured sizes, never
// estimates, because that number is what the decision is made on.
type Root struct {
	ID          config.RootID
	Dir         string
	Relocatable bool
	// PinnedBy names the environment variable holding this root, "" when none
	// does. A root that is pinned cannot be moved from the application.
	PinnedBy string
	Bytes    int64
	Files    int64
	// Missing is a root that has never been written. It is not an error: a
	// fresh install has no worktrees, and reporting zero is the honest answer.
	Missing bool
	// Truncated is a walk that hit Budget before finishing: Bytes and Files
	// are a lower bound, not the total.
	Truncated bool
	// Pending is a root Layout described but nobody has measured yet.
	Pending bool
	// Err is set when the walk could not finish. Bytes and Files then describe
	// what was reachable, so a permission-denied subtree understates rather
	// than blanks the report.
	Err string
	// Volume describes the filesystem Dir sits on. Roots sharing a volume
	// report the same one, which is what lets a reader see that moving one of
	// them buys nothing.
	Volume Volume
}

// Volume is the free/total pair for the filesystem under a path, plus the
// mount point (a drive on Windows) so a reader recognises which disk it is.
type Volume struct {
	Path  string
	Free  int64
	Total int64
}

// Budget is how long one root may be walked. A root that exceeds it reports
// what was counted so far with Truncated set, so a huge or stalled directory
// costs the user one row's precision rather than the whole panel.
var Budget = 10 * time.Second

// walkDir is the directory walk, held in a variable so a test can stand a slow
// or unbounded filesystem in its place. A walk reads it once, when it starts.
var walkDir = filepath.WalkDir

// Layout resolves every declared root without reading any of them: where each
// lives, whether it may move, and what volume it sits on. Sizes are left at
// zero with Pending set, so a caller can draw the rows now and fill them in as
// each root is measured.
func Layout() []Root {
	ids := config.RootIDs()
	out := make([]Root, 0, len(ids))
	volumes := map[string]Volume{}
	for _, id := range ids {
		root := describe(id)
		if root.Dir != "" {
			root.Volume = volumeFor(volumes, root.Dir)
			root.Pending = true
		}
		out = append(out, root)
	}
	return out
}

// Survey measures every declared root, each within Budget. It never fails as a
// whole: a root that cannot be read carries its own Err, because a single
// unreadable directory must not deny the user the numbers for the rest.
func Survey(ctx context.Context) []Root {
	ids := config.RootIDs()
	out := make([]Root, 0, len(ids))
	volumes := map[string]Volume{}
	for _, id := range ids {
		root := measured(ctx, describe(id), Budget)
		if root.Dir != "" {
			root.Volume = volumeFor(volumes, root.Dir)
		}
		out = append(out, root)
	}
	return out
}

// SurveyRoot measures one root, so a slow root never holds back the others.
// ok is false when id is not a declared root.
func SurveyRoot(ctx context.Context, id config.RootID) (Root, bool) {
	if !slices.Contains(config.RootIDs(), id) {
		return Root{}, false
	}
	root := measured(ctx, describe(id), Budget)
	if root.Dir != "" {
		root.Volume = readVolume(root.Dir)
	}
	return root, true
}

func describe(id config.RootID) Root {
	return Root{
		ID:          id,
		Dir:         config.RootDir(id),
		Relocatable: config.RootRelocatable(id),
		PinnedBy:    config.RootPinnedBy(id),
	}
}

func volumeFor(cache map[string]Volume, dir string) Volume {
	key := volumeKey(dir)
	vol, ok := cache[key]
	if !ok {
		vol = readVolume(dir)
		cache[key] = vol
	}
	return vol
}

func measured(ctx context.Context, root Root, budget time.Duration) Root {
	if root.Dir == "" {
		root.Missing = true
		return root
	}
	root.Bytes, root.Files, root.Missing, root.Truncated, root.Err = measureRoot(ctx, root.ID, root.Dir, budget)
	return root
}

// tally is the running count of one walk. It is read by whoever gave up
// waiting, while the walk may still be inside a call that cannot be interrupted.
type tally struct {
	walk         func(string, fs.WalkDirFunc) error
	bytes, files atomic.Int64
	mu           sync.Mutex
	err          string
}

func (t *tally) fail(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err == "" {
		t.err = text
	}
}

func (t *tally) firstErr() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// measureRoot sizes what a root owns within budget (zero: unlimited, which a
// move plan needs). The walk runs off to the side so a filesystem call that
// never returns still lets the caller answer with what was counted.
func measureRoot(ctx context.Context, id config.RootID, dir string, budget time.Duration) (bytes, files int64, missing, truncated bool, errText string) {
	bctx, cancel := ctx, context.CancelFunc(func() {})
	if budget > 0 {
		bctx, cancel = context.WithTimeout(ctx, budget)
	}
	defer cancel()
	count := tally{walk: walkDir}
	done := make(chan bool, 1)
	go func() { done <- measureOwned(bctx, id, dir, &count) }()
	select {
	case missing = <-done:
	case <-bctx.Done():
		missing = false
	}
	errText = count.firstErr()
	switch {
	case ctx.Err() != nil:
		if errText == "" {
			errText = ctx.Err().Error()
		}
	case bctx.Err() != nil:
		truncated = true
	}
	return count.bytes.Load(), count.files.Load(), missing && !truncated, truncated, errText
}

// A root sharing its directory with another counts only its declared entries,
// or the report would credit state with the configuration sitting beside it.
func measureOwned(ctx context.Context, id config.RootID, dir string, count *tally) bool {
	owned := config.RootOwns(id)
	if len(owned) == 0 {
		return measure(ctx, dir, count)
	}
	missing := true
	for _, name := range owned {
		if !measure(ctx, filepath.Join(dir, name), count) {
			missing = false
		}
	}
	return missing
}

// measure walks dir into count. It counts what it can reach and records the
// first refusal rather than aborting: an unreadable subtree is a smaller number
// plus a reason, which is more use than no number at all.
func measure(ctx context.Context, dir string, count *tally) (missing bool) {
	// Asked before the walk rather than inferred from it: a root nothing has
	// written yet and a root that refused to be read are different answers,
	// and the walk reports both as the same first callback error.
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return true
		}
		count.fail(err.Error())
		return false
	}
	walkErr := count.walk(dir, func(_ string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			count.fail(err.Error())
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			count.fail(infoErr.Error())
			return nil
		}
		count.files.Add(1)
		count.bytes.Add(info.Size())
		return nil
	})
	if walkErr != nil {
		count.fail(walkErr.Error())
	}
	return false
}

// volumeKey groups roots that sit on one filesystem, so the free-space probe
// runs once per disk instead of once per root.
func volumeKey(dir string) string {
	vol := filepath.VolumeName(dir)
	if vol != "" {
		return strings.ToLower(vol)
	}
	return "/"
}

// LeftBehind names the entries an earlier relocation left in the previous root,
// and where they still are. Boot brings them across on an ordinary install; a
// run whose roots are redirected by environment refuses that, on the rule that
// such a run asked for its own copies — so the panel says where they went
// rather than leaving a wallpaper and a set of rollback backups quietly gone.
func LeftBehind() (dir string, names []string) {
	home, state := config.ReasonixHomeDir(), config.MemoryUserDir()
	if home == "" || state == "" || strings.EqualFold(filepath.Clean(home), filepath.Clean(state)) {
		return "", nil
	}
	for _, name := range config.StateRootEntriesEarlyMovesLeft {
		if _, err := os.Lstat(filepath.Join(home, name)); err == nil {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", nil
	}
	return home, names
}

func isDirAt(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
