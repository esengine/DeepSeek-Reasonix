// Package isolation runs delegated work in a git worktree of the workspace and
// holds what it produced until the parent applies or discards it. The work
// never touches the workspace while it runs; applying it is an ordinary,
// observed write that is refused path by path where the workspace moved on.
package isolation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

// Refusal codes an isolated run or its settlement answers with.
const (
	CodeUnavailable   = "isolation.unavailable"
	CodeNotGit        = "isolation.not_git"
	CodeUnknownID     = "isolation.unknown_id"
	CodeApplyConflict = "isolation.apply_conflict"
)

var errUnavailable = tool.Refusal{Code: CodeUnavailable, Message: "worktree isolation is not available in this session"}

// Entry is an isolated environment and its current execution identity.
type Entry struct {
	ID              string
	Workspace       string
	Snap            worktree.Snapshot
	Cand            worktree.Candidate
	Tree            string
	Changes         []worktree.Change
	Stats           []worktree.FileStat
	State           State
	CleanupComplete bool
	ResumeOwner     string
	RunGeneration   string
	Evidence        *CandidateEvidence
	Environment     *worktree.EnvironmentReceipt
	Baseline        *evidence.ContentBaseline
	DiscardContent  *evidence.ContentSnapshot
	DiscardApproval string
	DiscardPolicy   string
	Origin          *SnapshotOrigin
	Lineage         *ResultLineage
	legacy          bool
	releaseRun      func()
}

// Store reads the durable lifecycle and settlement records of one workspace.
// Candidate files remain until settlement; terminal receipts survive cleanup.
type Store struct {
	managedRoot                  string
	workspace                    string
	repo                         gitcmd.Repo
	mutations                    *mutation.Coordinator
	environmentPaths             []string
	environmentDenied            []string
	preparationProbe             func(State, *Entry)
	cleanupProbe                 func(*Entry) error
	promotionProbe               func(*Entry) error
	childRegistrationProbe       func(*Entry, ChildResult)
	integrationRegistrationProbe func(*Entry)
	checkVerifier                func(evidence.CheckInputs, string) bool
}

// NewStore keeps worktrees under managedRoot and answers for results taken
// from workspaceRoot, whose git identity repo was resolved when it opened.
func NewStore(managedRoot, workspaceRoot string, repo gitcmd.Repo) *Store {
	return &Store{managedRoot: managedRoot, workspace: workspaceRoot, repo: repo}
}

func (s *Store) WithMutationCoordinator(coordinator *mutation.Coordinator) *Store {
	s.mutations = coordinator
	return s
}

func (s *Store) WithEnvironmentCopies(paths []string, denied ...string) *Store {
	s.environmentPaths = slices.Clone(paths)
	s.environmentDenied = slices.Clone(denied)
	return s
}

// Begin checks the workspace out into a fresh worktree. A workspace git cannot
// snapshot is refused with CodeNotGit rather than run unisolated.
func (s *Store) Begin(ctx context.Context, workspaceRoot string) (*Entry, error) {
	return s.BeginOwned(ctx, workspaceRoot, "")
}

func (s *Store) BeginOwned(ctx context.Context, workspaceRoot, owner string) (*Entry, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return s.beginOwned(ctx, workspaceRoot, owner, id, nil)
}

func (s *Store) beginOwned(ctx context.Context, workspaceRoot, owner, id string, lineage *ResultLineage) (*Entry, error) {
	if s == nil {
		return nil, errUnavailable
	}
	snap, err := s.takeSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	cand, err := worktree.PlanCandidate(snap, s.managedRoot)
	if err != nil {
		return nil, err
	}
	e := &Entry{ID: id, Workspace: workspaceRoot, Snap: snap, Cand: cand, State: Preparing, ResumeOwner: owner, Lineage: lineage}
	if s.mutations != nil {
		e.DiscardPolicy = hostDiscardPolicy
	}
	prepared, err := s.prepareEntry(ctx, e)
	if err != nil {
		_ = os.Remove(filepath.Dir(cand.WorktreeRoot))
		return nil, fmt.Errorf("isolated preparation %s failed: %w", id, err)
	}
	return prepared, nil
}

// Finish records a run's changes. Unchanged owned environments remain resumable;
// an unchanged legacy run leaves no entry and reports false.
func (s *Store) Finish(ctx context.Context, e *Entry) (bool, error) {
	return s.finishState(ctx, e, Pending)
}

func (s *Store) finishState(ctx context.Context, e *Entry, state State) (bool, error) {
	if e.releaseRun == nil {
		return false, tool.Refusal{Code: CodeBusy, Message: "isolated execution no longer owns this run"}
	}
	defer s.endRun(e)
	lease, err := s.holdCandidate(ctx, e, "isolation.finish")
	if err != nil {
		return false, s.failedFinish(ctx, e, err)
	}
	defer lease.End(mutation.Unknown)
	unlock, err := s.lockEntry(ctx, e.ID)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := s.checkRunGeneration(e); err != nil {
		return false, err
	}
	if e.Lineage != nil {
		if err := s.sealChildrenLocked(e); err != nil {
			return false, interruptFinishedParent(e, err)
		}
	}
	if e.releaseRun == nil {
		return false, tool.Refusal{Code: CodeBusy, Message: "isolated execution no longer owns this run"}
	}
	tree, changes, err := worktree.CandidateTree(ctx, e.Snap, e.Cand)
	if err == nil && len(changes) > 0 {
		e.Stats, err = worktree.DiffStat(ctx, e.Snap, tree, changes)
	}
	if err != nil {
		e.State = RecoveryRequired
		return false, errors.Join(err, persist(e))
	}
	if len(changes) == 0 && e.ResumeOwner == "" && e.Baseline.OnlyPathsChanged(ctx, e.Cand.WorktreeRoot, nil, 50_000) {
		return false, s.remove(ctx, e)
	}
	e.Tree, e.Changes = tree, changes
	if e.Evidence == nil {
		e.Evidence = &CandidateEvidence{EnvironmentID: e.ID, Workspace: e.Cand.WorkspaceRoot}
	}
	captureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	content := evidence.CaptureContentSnapshot(captureCtx, e.Cand.WorktreeRoot, 50_000)
	cancel()
	e.Evidence.FinishedContent = &content
	e.Evidence.Verification = "unknown"
	e.State = state
	if err := persist(e); err != nil {
		return false, err
	}
	return true, nil
}

// Abort removes a run's worktree without recording anything.
func (s *Store) Abort(ctx context.Context, e *Entry) {
	unlock, err := s.lockEntry(ctx, e.ID)
	if err != nil {
		return
	}
	defer unlock()
	if err := s.checkRunGeneration(e); err != nil {
		return
	}
	s.endRun(e)
	release, err := filelock.TryAcquire(s.runLock(e.ID))
	if err != nil {
		return
	}
	defer release()
	_ = s.remove(ctx, e)
}

// Paths is where an entry's changes land in the workspace.
func (s *Store) Paths(id string) ([]string, error) {
	e, err := s.operationSnapshot(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return integrationPaths(e)
}

// Apply writes an entry's changes into the workspace and retains its receipt. A path
// the workspace changed since the run began refuses the whole apply and keeps
// the entry, so the parent can reconcile and try again or discard it.
func (s *Store) Apply(ctx context.Context, id string) (*Entry, error) {
	before, err := s.operationSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	if done, err := settledOperation(before, Integrated); done || err != nil {
		return before, err
	}
	ctx, lease, err := s.holdIntegration(ctx, before)
	if err != nil {
		return nil, err
	}
	defer lease.End(mutation.Unknown)
	e, unlock, err := s.lockCurrent(ctx, before)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := settledOperation(e, Integrated); err != nil {
		return nil, err
	}
	if err := s.checkImportedPublication(e); err != nil {
		return nil, err
	}
	if e.State == Integrated {
		return e, s.remove(ctx, e)
	}
	if !settleable(e.State) && e.State != Applying {
		return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "isolated result requires recovery before it can be applied"}
	}
	if !onlyPublishableChanges(ctx, e) {
		return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "candidate has unpublished or unknown environment changes; retain and reconcile them before applying, or explicitly discard this result"}
	}
	if e.Origin != nil {
		if err := worktree.ValidatePublicationSourceModes(ctx, e.Snap, e.Origin.Materialization, e.Changes); err != nil {
			return nil, err
		}
	}
	previousState := e.State
	e.State = Applying
	if err := persist(e); err != nil {
		return nil, err
	}
	if err := worktree.ApplyPaths(worktree.WithApplyIdentity(ctx, e.ID, ""), e.Snap, e.Tree, e.Changes); err != nil {
		if errors.Is(err, worktree.ErrApplyRecovery) {
			return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "integration interrupted; result and recovery journal retained: " + err.Error()}
		}
		e.State = previousState
		var conflict *worktree.ConflictError
		if errors.As(err, &conflict) {
			e.State = Conflict
		}
		if saveErr := persist(e); saveErr != nil {
			return nil, errors.Join(err, saveErr)
		}
		if errors.As(err, &conflict) {
			return nil, tool.Refusal{Code: CodeApplyConflict, Message: fmt.Sprintf(
				"nothing was applied: the workspace changed %s since %s began; reconcile and apply again, or discard it",
				strings.Join(conflict.Paths, ", "), id)}
		}
		return nil, err
	}
	e.State = Integrated
	if err := persist(e); err != nil {
		return nil, err
	}
	return e, s.remove(ctx, e)
}

// Discard removes a candidate and retains the decision for idempotent retries.
func (s *Store) Discard(ctx context.Context, id string) (*Entry, error) {
	return s.discard(ctx, id, "")
}

func (s *Store) discard(ctx context.Context, id, revision string) (*Entry, error) {
	before, err := s.discardSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	if revision == "" && before.DiscardPolicy == hostDiscardPolicy {
		return nil, tool.Refusal{Code: "isolation.user_action_required", Message: "discard this retained result through the host result manager after reviewing its current preview"}
	}
	if done, err := settledOperation(before, Discarded); done || err != nil {
		return before, err
	}
	lease, err := s.holdCandidate(ctx, before, "isolation.discard")
	if err != nil {
		return nil, err
	}
	defer lease.End(mutation.Unknown)
	e, unlock, err := s.lockCurrentUsing(ctx, before, s.discardManifest)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if done, err := settledOperation(e, Discarded); done || err != nil {
		return e, err
	}
	if e.State == Applying {
		return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "finish or recover integration before discarding its candidate"}
	}
	if revision != "" && (e.State != Discarded || e.DiscardApproval != revision) {
		preview, content, err := inspectDiscard(ctx, e)
		if err != nil {
			return nil, err
		}
		if preview.Revision != revision {
			return nil, tool.Refusal{Code: CodeStaleDiscard, Message: "candidate changed after the discard preview; refresh before deciding"}
		}
		e.DiscardContent = &content
		e.DiscardApproval = revision
	} else if e.State != Discarded {
		content := evidence.CaptureContentSnapshot(ctx, e.Cand.WorktreeRoot, 50_000)
		if content.Complete {
			e.DiscardContent = &content
		} else if s.mutations != nil {
			return nil, tool.Refusal{Code: CodeRecoveryRequired, Message: "candidate contents are not completely observed; files retained"}
		}
	}
	saved, err := s.operationManifest(e.ID)
	if err != nil {
		return nil, err
	}
	if settleable(saved.State) || saved.State == RecoveryRequired {
		e.Tree, e.Changes, e.Stats = saved.Tree, saved.Changes, saved.Stats
	}
	e.State = Discarded
	if err := persist(e); err != nil {
		return nil, err
	}
	return e, s.remove(ctx, e)
}

// Pending lists this workspace's results still waiting on the parent, by id.
func (s *Store) Pending() []string {
	if s == nil {
		return nil
	}
	ids := s.durableIDs()
	slices.Sort(ids)
	return slices.Compact(ids)
}

func (s *Store) lookup(id string) (*Entry, error) {
	if s == nil {
		return nil, errUnavailable
	}
	id = strings.TrimSpace(id)
	return s.recover(id)
}

func (s *Store) remove(ctx context.Context, e *Entry) error {
	if err := s.requireSettledChildren(e); err != nil {
		return err
	}
	if terminalState(e.State) && e.CleanupComplete {
		return nil
	}
	if terminalState(e.State) {
		if e.State == Discarded && !discardContentUnchanged(ctx, e) && (e.DiscardContent != nil || s.mutations != nil) {
			if _, err := os.Lstat(e.Cand.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
				return tool.Refusal{Code: CodeStaleDiscard, Message: "candidate changed after discard approval; files retained until a fresh explicit decision"}
			}
		}
		if e.State == Integrated && !sealedContentUnchanged(ctx, e) {
			if _, err := os.Lstat(e.Cand.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
				return tool.Refusal{Code: CodeRecoveryRequired, Message: "integrated result retains unpublished or unknown environment contents; cleanup refused"}
			}
		}
		if err := s.promoteSettled(e); err != nil {
			return err
		}
	}
	if err := worktree.RemoveCandidate(context.WithoutCancel(ctx), e.Snap, e.Cand); err != nil {
		return err
	}
	if terminalState(e.State) {
		if s.cleanupProbe != nil {
			if err := s.cleanupProbe(e); err != nil {
				return err
			}
		}
		e.CleanupComplete = true
		if err := persist(e); err != nil {
			return err
		}
		return nil
	}
	if err := os.Remove(entryRecordPath(e)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(filepath.Dir(e.Cand.WorktreeRoot))
	return nil
}

func settledOperation(e *Entry, requested State) (bool, error) {
	if terminalState(e.State) && e.State != requested {
		return false, tool.Refusal{Code: CodeAlreadySettled, Message: "isolated result already " + string(e.State) + "; no changes made"}
	}
	return terminalState(e.State) && e.CleanupComplete, nil
}

func newID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "iso_" + hex.EncodeToString(b[:]), nil
}
