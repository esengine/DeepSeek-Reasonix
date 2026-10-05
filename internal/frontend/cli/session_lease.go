package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/session/control"
)

// sessionLeaseResumeRefusal is the startup-time refusal for `reasonix
// [--resume|--continue]` and `reasonix run --resume/--continue`: it names the
// holder and offers the two ways out (close the holder, or continue in a
// duplicated session via --copy).
func sessionLeaseResumeRefusal(err error, takeover bool) string {
	if takeover {
		return control.SessionInUseMessage(err) +
			"; --takeover cannot take a session from another Reasonix process: close it, or rerun with --copy to continue in a duplicated session"
	}
	return control.SessionInUseMessage(err) +
		"; close the other Reasonix window or process, or rerun with --copy to continue in a duplicated session"
}

// cliSessionRecoveredHandler moves the single-session CLI lease during the
// controller's recovery commit. The callback runs before Controller changes its
// session path, closing the unguarded interval that event-driven follow-up
// calls left after ordinary turn-end and mid-turn autosaves.
func cliSessionRecoveredHandler(leases *control.SessionLeaseKeeper) func(control.SessionRecoveryInfo) error {
	return func(info control.SessionRecoveryInfo) error {
		if err := leases.HandleSessionRecovered(info); err != nil {
			return err
		}
		// Controller pointer is not available here; TUI followSessionLease and
		// headless post-Rebind bind authority. Recovery commit rebinds the lease
		// first; the next Snapshot path match is ensured once Bind runs.
		return nil
	}
}

func rebindCLIControllerAuthority(leases *control.SessionLeaseKeeper, ctrl *control.Controller) error {
	if leases == nil || ctrl == nil {
		return nil
	}
	if err := leases.Rebind(ctrl.SessionPath()); err != nil {
		return err
	}
	return leases.BindControllerAuthority(ctrl)
}

// copySessionForWriting duplicates the session at src into a fresh session
// file beside it and returns the new path. It backs the --copy escape hatch:
// when src is held by another runtime, the copy gives this process a session
// it can own. The duplicate is written through Session.SaveIfAbsent, so it is
// event-log aware (authoritative event log plus .jsonl checkpoint), cannot
// replace a destination another runtime created, and starts with no
// lease/lock sidecars of its own; src is only read. When src is being
// written concurrently, the copy captures the transcript as of the load — an
// append-only prefix, the same view a resume would see.
func copySessionForWriting(src string) (string, error) {
	loaded, err := loadResumableSession(src)
	if err != nil {
		return "", err
	}
	msgs := loaded.Snapshot()

	var srcMeta sessionstore.BranchMeta
	if meta, ok, metaErr := sessionstore.LoadBranchMeta(src); metaErr == nil && ok {
		srcMeta = meta
	}
	label := "session"
	if model, ok := sessionstore.LoadSessionModel(src); ok && strings.TrimSpace(model) != "" {
		label = model
	}

	newPath := sessionstore.NewSessionPath(filepath.Dir(src), label)
	copySess := sessionstore.NewSession("")
	copySess.Messages = msgs
	if err := copySess.SaveIfAbsent(newPath); err != nil {
		return "", fmt.Errorf("copy session: %w", err)
	}
	preview, turns := sessionstore.SessionPreviewFromMessages(msgs)
	meta := sessionstore.BranchMeta{
		ParentID:      sessionstore.BranchID(src),
		Preview:       preview,
		Turns:         turns,
		SchemaVersion: sessionstore.BranchMetaCountsVersion,
		Model:         srcMeta.Model,
	}
	if title := strings.TrimSpace(firstNonEmpty(srcMeta.CustomTitle, srcMeta.TopicTitle)); title != "" {
		meta.CustomTitle = title + " (copy)"
	}
	if err := sessionstore.SaveBranchMeta(newPath, meta); err != nil {
		return "", fmt.Errorf("copy session meta: %w", err)
	}
	return newPath, nil
}

// bindRunSession attaches a headless run to its session file — an explicit
// --resume path, otherwise a fresh one — and takes the write lease a turn
// needs. A fresh path is defensively rebound too; a resumed one already holds
// the lease, making that a no-op.
func bindRunSession(ctrl *control.Controller, leases *control.SessionLeaseKeeper, resumed *sessionstore.Session, resumePath string) error {
	if resumePath != "" {
		_ = ctrl.Resume(resumed, resumePath) // startup: nothing can be running
	}
	if ctrl.SessionPath() == "" && ctrl.SessionDir() != "" {
		ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	}
	if err := rebindCLIControllerAuthority(leases, ctrl); err != nil {
		return err
	}
	reclaimCLIRecoveryBranches(ctrl.SessionDir())
	reportInterruptedAdjudications(ctrl)
	return nil
}

// reportInterruptedAdjudications says why a resumed session stops where it
// does. A run that died waiting on a person leaves no sign in the transcript,
// so without this the terminal shows a conversation that simply ends. It is
// provenance, not a prompt: the question cannot be answered any more, and
// nothing it was holding back was executed.
func reportInterruptedAdjudications(ctrl *control.Controller) {
	active, _ := ctrl.Adjudications()
	for _, item := range active {
		question := item.Summary
		if question == "" {
			question = item.Kind
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", i18n.M.InterruptedAdjudicationPrefix, question)
	}
}
