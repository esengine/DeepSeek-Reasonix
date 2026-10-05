// preflight.go — what has to be true before a root may be moved.
package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/textutil"
	"reasonix/internal/contract/config"
)

// Refusal is one reason a move cannot proceed. Code is stable for the caller to
// branch on; Detail is the sentence a person reads. Both travel together so a
// surface never has to reconstruct the explanation from the code.
type Refusal struct {
	Code   string
	Detail string
}

// Plan is what a move would do, decided before anything is written. A plan with
// refusals is not a failure to compute — it is the answer, and the caller shows
// it rather than starting work that cannot finish.
type Plan struct {
	Root  config.RootID
	From  string
	To    string
	Bytes int64
	Files int64
	// Need and Free are the same units the refusal below is decided on, so a
	// surface can show the shortfall without recomputing it.
	Need int64
	Free int64
	// Adopt reports that the target already holds this root's own data, so the
	// move is the pointer alone: nothing is copied, verified or reclaimed, and
	// Bytes then describes what is being adopted rather than what is copied.
	Adopt bool
	// Stays is what an adopt leaves at the current location. Anything written
	// there since is not carried across, and a surface says so rather than
	// letting it go quietly missing.
	Stays    int64
	Refusals []Refusal
}

// OK reports whether the plan may be committed.
func (p Plan) OK() bool { return len(p.Refusals) == 0 }

// headroom is the slack a move leaves on the target beyond the bytes it copies.
// A move that fills the destination to the last byte leaves a machine that
// cannot write the transcript it was moved to protect.
const headroom = 256 << 20

// PlanMove decides whether root may move to target and what it would cost. It
// writes nothing. Every refusal it can find is collected rather than the first:
// a user picking a drive wants both "too small" and "not writable" at once, not
// one per attempt.
func PlanMove(ctx context.Context, root config.RootID, target string) Plan {
	plan := Plan{Root: root, To: strings.TrimSpace(target)}

	if !config.RootRelocatable(root) {
		plan.Refusals = append(plan.Refusals, Refusal{
			Code:   "root.immovable",
			Detail: fmt.Sprintf("%s has to stay where every instance can find it", root),
		})
		return plan
	}
	if pin := config.RootPinnedBy(root); pin != "" {
		plan.Refusals = append(plan.Refusals, Refusal{
			Code:   "root.pinned",
			Detail: fmt.Sprintf("%s is set in the environment; unset it to choose a location here", pin),
		})
		return plan
	}
	plan.From = config.RootDir(root)
	if plan.From == "" {
		plan.Refusals = append(plan.Refusals, Refusal{
			Code:   "source.unresolved",
			Detail: "this machine has no resolvable location for that data",
		})
		return plan
	}
	if plan.To == "" {
		plan.Refusals = append(plan.Refusals, Refusal{Code: "target.empty", Detail: "choose a folder to move it to"})
		return plan
	}
	plan.To = filepath.Clean(plan.To)

	if samePath(plan.From, plan.To) {
		plan.Refusals = append(plan.Refusals, Refusal{Code: "target.same", Detail: "that is where it already is"})
		return plan
	}
	// A target inside the source would have the copy walking into its own
	// output; a source inside the target would delete the destination when the
	// move reclaims it. Both are the same mistake seen from two sides.
	if within(plan.To, plan.From) {
		plan.Refusals = append(plan.Refusals, Refusal{
			Code: "target.inside_source", Detail: "that folder is inside the one being moved",
		})
	}
	if within(plan.From, plan.To) {
		plan.Refusals = append(plan.Refusals, Refusal{
			Code: "source.inside_target", Detail: "the folder being moved is inside that one",
		})
	}

	// What the move would copy, which for a root sharing its directory is its
	// declared entries rather than everything under them.
	plan.Bytes, plan.Files, _, _, _ = measureRoot(ctx, plan.Root, plan.From, 0)
	plan.Need = plan.Bytes + headroom

	if err := probeWritable(plan.To); err != nil {
		plan.Refusals = append(plan.Refusals, Refusal{
			Code: "target.unwritable", Detail: "that folder cannot be written to: " + err.Error(),
		})
		return plan
	}
	// A folder with something in it is refused, unless that something is this
	// root's own: an interrupted move claimed it, which the journal records, or
	// a finished one did, which the folder itself says.
	if entries, err := os.ReadDir(plan.To); err == nil && occupied(entries) && !resumes(plan) {
		if holdsRoot(plan.Root, plan.To, entries) {
			plan.Adopt = true
			plan.Stays = plan.Bytes
			plan.Bytes, plan.Files, _, _, _ = measureRoot(ctx, plan.Root, plan.To, 0)
			plan.Need = headroom
		} else {
			plan.Refusals = append(plan.Refusals, Refusal{
				Code: "target.not_empty",
				Detail: "that folder holds something else; choose an empty one, " +
					"or one that already holds this data and it will be pointed at as it is",
			})
		}
	}
	vol := readVolume(plan.To)
	plan.Free = vol.Free
	if vol.Free > 0 && vol.Free < plan.Need {
		plan.Refusals = append(plan.Refusals, Refusal{
			Code: "target.too_small",
			Detail: fmt.Sprintf("that drive has %s free; the move needs %s",
				HumanBytes(vol.Free), HumanBytes(plan.Need)),
		})
	}
	return plan
}

// occupied reports whether a directory holds anything but a marker. A folder
// left with only the marker of a root that has since moved away holds no data,
// so it is an empty target rather than one to be adopted or refused.
func occupied(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if entry.Name() != markerName {
			return true
		}
	}
	return false
}

// resumes reports whether an unfinished move already claimed this exact
// destination for this exact root.
func resumes(plan Plan) bool {
	pending, ok := PendingMove()
	return ok && pending.Root == plan.Root && samePath(pending.To, plan.To)
}

// probeWritable creates the target if it is missing and proves a file can be
// written in it. Checking the mode bits instead would pass on a read-only
// network share and fail halfway through the copy.
func probeWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".reasonix-write-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	closeErr := probe.Close()
	rmErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return rmErr
}

// within reports whether path sits inside base, comparing cleaned absolute
// paths so "D:\a\..\b" cannot pose as a different tree.
func within(path, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// HumanBytes renders a size the way the storage panel shows it.
func HumanBytes(n int64) string { return textutil.HumanBytes(n) }
