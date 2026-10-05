// Keeping the criteria a turn is about to overwrite. The bytes are taken from
// the pre-image the host already had in hand, never re-read from the workspace
// afterwards: by then the only copy left is the one the rewrite produced.
package agent

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/diff"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/safety/evidence"
)

// captureRewrittenCriteria files what a criterion said before this edit. It runs
// at preview time because that is the last moment the old bytes exist anywhere
// the host can reach — the workspace is about to stop holding them, and nothing
// may reconstruct them from what execution was allowed to write.
func (a *Agent) captureRewrittenCriteria(change diff.Change, rewritten []string) {
	if a == nil {
		return
	}
	store := a.baselineCriteriaStore()
	if store == nil || len(rewritten) == 0 || change.OldText == "" || !evidence.PathMayHoldTestCriteria(change.Path) {
		return
	}
	criterion, err := store.Capture(change.Path, []byte(change.OldText))
	if err != nil {
		// Losing the capture is not a reason to refuse the edit; it is a reason
		// the host cannot later answer for what the criterion used to say.
		slog.Warn("baseline criteria: capture failed", "path", change.Path, "err", err)
		return
	}
	if a.task.baselineCriteria == nil {
		a.task.baselineCriteria = map[string]evidence.TestCriterion{}
	}
	// The first capture wins: later edits rewrite what a previous edit already
	// replaced, and the task is held to what it began with.
	if _, held := a.task.baselineCriteria[change.Path]; !held {
		a.task.baselineCriteria[change.Path] = criterion
	}
}

// baselineCriteriaStore addresses the captures. It is built on demand because
// the root is host state the Agent already carries.
func (a *Agent) baselineCriteriaStore() *evidence.BaselineStore {
	return evidence.NewBaselineStore(baselineCriteriaRoot(strings.TrimSpace(a.archiveDir)))
}

// baselineCriteriaRoot keeps captures beside the turn archive, which is host
// state outside the workspace. Inside it, execution could rewrite the record of
// what it rewrote.
func baselineCriteriaRoot(archiveDir string) string {
	if archiveDir == "" {
		return ""
	}
	return filepath.Join(archiveDir, "baseline-criteria")
}

// guaranteesBaselineProvenance reports whether this session promised that the
// criteria a task began under stay the host's. It is one choice made for the
// session, never a per-call downgrade: a guarantee that quietly lapses when the
// store is unwritable is worse than one that was never made.
func (a *Agent) guaranteesBaselineProvenance() bool {
	return a != nil && a.deliveryProfile
}

// captureCriteriaBefore holds what a call could overwrite before it may run. A
// writer names what it will touch; a call whose scope the host cannot read
// could touch anything writable. Failing to hold a criterion refuses the call
// only where the session promised to keep it: the bytes exist until this runs,
// and a provenance loss is the one debt nothing afterwards can settle.
func (a *Agent) captureCriteriaBefore(ctx context.Context, plan *toolCallPlan) error {
	if a == nil || plan == nil {
		return nil
	}
	store := a.baselineCriteriaStore()
	if evidence.ToolCallMutationClassWith(plan.evidenceName, plan.evidenceArgs, plan.facts()) == evidence.MutationUnknown {
		return a.captureCriteriaOnce(ctx, store)
	}
	for _, path := range evidence.ToolCallPaths(plan.evidenceArgs) {
		if err := a.captureCriterionAt(store, path); err != nil {
			return err
		}
	}
	return nil
}

// captureCriteriaOnce walks the writable domain only where it moved since the
// last walk. Which criteria exist answers to the state, not the call, and
// BaselineTestObligations already indexes the evaluation half that way; the
// capture half re-derived it per call — 6.5s each on a 650k-file root. A
// mutation the host could not prove clean still moves the epoch.
func (a *Agent) captureCriteriaOnce(ctx context.Context, store *evidence.BaselineStore) error {
	epoch := a.mutationEpoch()
	if a.task.criteriaHeldAt(epoch) {
		return nil
	}
	if err := a.captureCriteriaUnder(ctx, store, a.writeWorkspaceRoot); err != nil {
		return err
	}
	a.task.noteCriteriaCaptured(epoch)
	return nil
}

// captureCriteriaUnder holds every criterion in the writable domain the host
// does not already have. A subtree it cannot enter is skipped, not refused —
// already what an unreadable file gets below, and the two disagreeing refused
// every write in a workspace whose drive root carried one system directory
// nobody can open.
func (a *Agent) captureCriteriaUnder(ctx context.Context, store *evidence.BaselineStore, root string) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	// A stop has to reach a walk that crosses the whole writable domain.
	checked := 0
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if checked++; checked%criteriaWalkCancelCheckEvery == 0 && ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if fileutil.IsVCSStoreDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		return a.captureCriterionAt(store, path)
	})
}

// criteriaWalkCancelCheckEvery bounds how much walking a stop waits on, the way
// scanWorkspace bounds its own.
const criteriaWalkCancelCheckEvery = 512

// captureCriterionAt files one path if it carries criteria the host does not
// hold. Already held comes first: a criterion captured earlier is safe whatever
// the store can do now, so a backend gone unwritable never re-blocks a change
// to something already kept. The map records a durable write, not a handle — a
// store that later loses its contents surfaces at evaluation, not here.
func (a *Agent) captureCriterionAt(store *evidence.BaselineStore, path string) error {
	// Before the read, not after: the name already settles whether this file
	// could hold a criterion, and asking the bytes instead read every file in
	// the workspace to answer it — on every broad-scope call of the task.
	if !evidence.PathMayHoldTestCriteria(path) {
		return nil
	}
	if _, held := a.task.baselineCriteria[path]; held {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil || !evidence.HoldsTestCriteria(path, content) {
		return nil
	}
	criterion, err := store.Capture(path, content)
	if err != nil {
		if !a.guaranteesBaselineProvenance() {
			// The session never promised to keep this. It proceeds, and claims
			// nothing about what the criterion used to say.
			return nil
		}
		return fmt.Errorf("hold %s before it is overwritten: %w", filepath.Base(path), err)
	}
	if a.task.baselineCriteria == nil {
		a.task.baselineCriteria = map[string]evidence.TestCriterion{}
	}
	a.task.baselineCriteria[path] = criterion
	return nil
}
