package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

var (
	// ErrSessionBusy marks a conversation somebody holds: open in a window, running a turn, or written by another process.
	ErrSessionBusy = errors.New("the conversation is held by a window or a process")
	// ErrSameFolder marks a move into the folder the conversation is already in.
	ErrSameFolder = errors.New("the conversation is already in that folder")
	// ErrMoveNotVerified marks a move whose copy did not read back identical; the copy is removed and the original left alone.
	ErrMoveNotVerified = errors.New("the moved conversation did not read back identical")
	// ErrSourceNotRemoved marks a move that arrived whole but could not take the original away.
	ErrSourceNotRemoved = errors.New("the conversation arrived but its original could not be removed")
)

// AdoptRecord says how a conversation came to be where it is. From is the
// transcript a move left, FromRoot the project it belonged to.
type AdoptRecord struct {
	Kind     string    `json:"kind"`
	From     string    `json:"from,omitempty"`
	FromRoot string    `json:"from_root,omitempty"`
	At       time.Time `json:"at"`
}

// AdoptInput is everything a conversation carries into a new home. What a
// project allowed (approval mode, goal, presets) is not here on purpose: the
// target project decides those for itself.
type AdoptInput struct {
	Messages  []provider.Message
	Title     string
	Model     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Pinned    bool
	Archived  bool
	Root      string
	Record    AdoptRecord
}

// AdoptConversation writes the messages as a new native session in dir. Nothing
// is left behind when any step fails.
func AdoptConversation(dir string, in AdoptInput) (string, error) {
	if in.Record.From != "" {
		if err := refuseBoundConversationTransfer(in.Record.From); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := NewSessionPath(dir, in.Model)
	sess := NewSession("")
	sess.Messages = in.Messages
	if err := sess.SaveIfAbsent(path); err != nil {
		return "", err
	}
	preview, turns := SessionPreviewFromMessages(in.Messages)
	created, updated := in.CreatedAt, in.UpdatedAt
	if created.IsZero() {
		created = time.Now()
	}
	if updated.IsZero() {
		updated = created
	}
	meta := BranchMeta{
		ID: BranchID(path), CreatedAt: created, UpdatedAt: updated, CustomTitle: in.Title, Pinned: in.Pinned, Archived: in.Archived,
		Model: in.Model, WorkspaceRoot: in.Root, Preview: preview, Turns: turns, SchemaVersion: BranchMetaCountsVersion, Adopted: &in.Record,
	}
	if in.Record.At.IsZero() {
		meta.Adopted.At = time.Now()
	}
	if err := SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		_ = store.RemoveSessionArtifacts(path)
		return "", err
	}
	return path, nil
}

// MoveSession re-homes a conversation into another project's session folder. It
// writes a new session from the messages and the person's own labels (title,
// pin, archive) and carries none of what the old project allowed, so the
// target's own trust and write roots decide everything from then on. The
// original goes only after the copy reads back identical.
func MoveSession(src, dstDir, dstRoot string) (string, error) {
	if err := refuseBoundConversationTransfer(src); err != nil {
		return "", err
	}
	if sameDir(filepath.Dir(src), dstDir) {
		return "", ErrSameFolder
	}
	guard, err := TryAcquireSessionRemovalGuard(src)
	if err != nil {
		var held *SessionLeaseError
		if errors.As(err, &held) {
			return "", errors.Join(ErrSessionBusy, err)
		}
		return "", err
	}
	msgs, _, intact, err := LoadSessionDisplayMessages(src)
	if err != nil {
		guard.Release()
		return "", err
	}
	if !intact {
		guard.Release()
		return "", ErrSessionDisplayReadModelDamaged
	}
	for _, message := range msgs {
		if message.AgentComposition != nil {
			guard.Release()
			return "", agentcomposition.ErrWorkspaceBound
		}
	}
	meta, _, _ := LoadBranchMeta(src)
	dst, err := AdoptConversation(dstDir, AdoptInput{
		Messages: withoutSystem(msgs), Title: meta.CustomTitle, Model: meta.Model, CreatedAt: meta.CreatedAt, UpdatedAt: meta.UpdatedAt,
		Pinned: meta.Pinned, Archived: meta.Archived, Root: dstRoot,
		Record: AdoptRecord{Kind: "move", From: src, FromRoot: meta.WorkspaceRoot, At: time.Now()},
	})
	if err != nil {
		guard.Release()
		return "", err
	}
	if back, _, ok, rerr := LoadSessionDisplayMessages(dst); rerr != nil || !ok {
		guard.Release()
		_ = store.RemoveSessionArtifacts(dst)
		return "", errors.Join(ErrMoveNotVerified, rerr)
	} else if want, werr := DigestSessionMessages(withoutSystem(msgs)); werr != nil {
		guard.Release()
		_ = store.RemoveSessionArtifacts(dst)
		return "", werr
	} else if digest, derr := DigestSessionMessages(back); derr != nil || digest != want {
		guard.Release()
		_ = store.RemoveSessionArtifacts(dst)
		return "", ErrMoveNotVerified
	}
	if err := removeWholeSession(src); err != nil {
		guard.Release()
		return dst, errors.Join(ErrSourceNotRemoved, err)
	}
	// A lock file that survives is bookkeeping: the conversation is gone.
	_ = guard.RemoveSidecarsAndRelease()
	return dst, nil
}

// withoutSystem leaves out the system messages: they hold the old project's
// own instructions, and the project that opens the conversation writes its own.
func withoutSystem(msgs []provider.Message) []provider.Message {
	out := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role != provider.RoleSystem {
			out = append(out, m)
		}
	}
	return out
}

// removeWholeSession takes a conversation away with everything beside it,
// including the directories that keep their own lifecycle while it lives.
func removeWholeSession(path string) error {
	errs := []error{store.RemoveSessionArtifacts(path)}
	for _, d := range []string{store.SessionCheckpointDir(path), store.SessionJobsDir(path), store.SessionInboxDir(path)} {
		if err := os.RemoveAll(d); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func sameDir(a, b string) bool {
	ca, errA := filepath.Abs(a)
	cb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && CanonicalSessionPath(ca) == CanonicalSessionPath(cb)
}
