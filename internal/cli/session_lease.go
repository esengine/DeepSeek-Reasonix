package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

// sessionLeaseResumeRefusal is the startup-time refusal for `reasonix
// [--resume|--continue]` and `reasonix run --resume/--continue`: it names the
// holder and offers the two ways out (close the holder, or continue in a
// duplicated session via --copy).
func sessionLeaseResumeRefusal(err error) string {
	return control.SessionInUseMessage(err) +
		"; close the other Reasonix window or process, or rerun with --copy to continue in a duplicated session"
}

// sessionLeaseHeldNotice is the in-TUI refusal for /resume and /switch, where
// exiting to rerun with --copy is not the natural move.
func sessionLeaseHeldNotice(err error) string {
	return control.SessionInUseMessage(err) + "; " + control.SessionLeaseCloseHint
}

// rebindSessionLease moves the chat TUI's session lease to path before the
// controller binds it for writing. A nil keeper (tests, persistence disabled)
// gates nothing. On error the keeper still guards the previous session.
func (m *chatTUI) rebindSessionLease(path string) error {
	if m.leases == nil {
		return nil
	}
	return m.leases.Rebind(path)
}

// restoreSessionLease re-points the lease at the controller's current session
// after a switch attempt moved it but the switch itself then failed.
// Best-effort: the old lease was released during the rebind, so in the
// (unlikely) case another runtime grabbed it in between this stays silent and
// the next write surfaces the conflict.
func (m *chatTUI) restoreSessionLease() {
	if m.leases == nil {
		return
	}
	_ = m.leases.Rebind(m.ctrl.SessionPath())
}

// followSessionLease re-points the TUI's session lease at the controller's
// current session file after an operation that rotated it to a fresh path
// (/new, /clear, /branch, fork). A fresh path cannot be held by anyone else,
// so failure is theoretical — but never silent.
func (m *chatTUI) followSessionLease() {
	if m.leases == nil {
		return
	}
	if err := m.leases.Rebind(m.ctrl.SessionPath()); err != nil {
		m.notice(sessionLeaseHeldNotice(err))
	}
}

// copySessionForWriting duplicates the session at src into a fresh session
// file beside it and returns the new path. It backs the --copy escape hatch:
// when src is held by another runtime, the copy gives this process a session
// it can own. The duplicate is written through Session.Save, so it is
// event-log aware (authoritative event log plus .jsonl checkpoint) and starts
// with no lease/lock sidecars of its own; src is only read. When src is being
// written concurrently, the copy captures the transcript as of the load — an
// append-only prefix, the same view a resume would see.
func copySessionForWriting(src string) (string, error) {
	loaded, err := loadResumableSession(src)
	if err != nil {
		return "", err
	}
	msgs := loaded.Snapshot()

	var srcMeta agent.BranchMeta
	if meta, ok, metaErr := agent.LoadBranchMeta(src); metaErr == nil && ok {
		srcMeta = meta
	}
	label := "session"
	if model, ok := agent.LoadSessionModel(src); ok && strings.TrimSpace(model) != "" {
		label = model
	}

	newPath := agent.NewSessionPath(filepath.Dir(src), label)
	copySess := agent.NewSession("")
	copySess.Messages = msgs
	if err := copySess.Save(newPath); err != nil {
		return "", fmt.Errorf("copy session: %w", err)
	}
	preview, turns := agent.SessionPreviewFromMessages(msgs)
	meta := agent.BranchMeta{
		ParentID:         agent.BranchID(src),
		ForkTurn:         -1,
		ForkMessageIndex: len(msgs),
		Preview:          preview,
		Turns:            turns,
		SchemaVersion:    agent.BranchMetaCountsVersion,
		Model:            srcMeta.Model,
	}
	if title := strings.TrimSpace(firstNonEmpty(srcMeta.CustomTitle, srcMeta.TopicTitle)); title != "" {
		meta.CustomTitle = title + " (copy)"
	}
	if err := agent.SaveBranchMeta(newPath, meta); err != nil {
		return "", fmt.Errorf("copy session meta: %w", err)
	}
	return newPath, nil
}
