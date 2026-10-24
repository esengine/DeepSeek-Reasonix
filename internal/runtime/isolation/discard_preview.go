package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

const CodeStaleDiscard = "isolation.discard_changed"
const hostDiscardPolicy = "host-preview-v1"

type DiscardPreview struct {
	ID            string `json:"id"`
	Revision      string `json:"revision"`
	CandidateRoot string `json:"candidateRoot"`
	Entries       int    `json:"entries"`
	State         State  `json:"state"`
}

// PreviewDiscard binds a human decision to the complete supported candidate.
// It neither changes lifecycle nor assumes the original manifest is still current.
func (s *Store) PreviewDiscard(ctx context.Context, id string) (DiscardPreview, error) {
	if strings.HasPrefix(id, "int_") {
		store, err := s.managementIntegrationStore(ctx, id)
		if err != nil {
			return DiscardPreview{}, err
		}
		return store.previewIntegrationDiscard(ctx, id)
	}
	store, err := s.managementStore(ctx, id)
	if err != nil {
		return DiscardPreview{}, err
	}
	return store.previewDiscardLocal(ctx, id)
}

func (s *Store) previewDiscardLocal(ctx context.Context, id string) (DiscardPreview, error) {
	before, err := s.discardSnapshot(ctx, id)
	if err != nil {
		return DiscardPreview{}, err
	}
	lease, err := s.holdCandidate(ctx, before, "isolation.discard.preview")
	if err != nil {
		return DiscardPreview{}, err
	}
	defer lease.End(mutation.NotExecuted)
	e, unlock, err := s.lockCurrentUsing(ctx, before, s.discardManifest)
	if err != nil {
		return DiscardPreview{}, err
	}
	defer unlock()
	preview, _, err := inspectDiscard(ctx, e)
	return preview, err
}

func inspectDiscard(ctx context.Context, e *Entry) (DiscardPreview, evidence.ContentSnapshot, error) {
	preview := DiscardPreview{ID: e.ID, CandidateRoot: e.Cand.WorktreeRoot, State: e.State}
	if e.State == Applying || e.State == Integrated {
		return preview, evidence.ContentSnapshot{}, tool.Refusal{Code: CodeRecoveryRequired, Message: "finish integration recovery before discarding this candidate"}
	}
	content := evidence.CaptureContentSnapshot(ctx, e.Cand.WorktreeRoot, 50_000)
	if !content.Complete {
		if !errors.Is(candidateAbsent(e), os.ErrNotExist) {
			return preview, content, tool.Refusal{Code: CodeRecoveryRequired, Message: "candidate cannot be fully observed for discard: " + content.Reason}
		}
		content = evidence.ContentSnapshot{}
	}
	raw, err := json.Marshal(struct {
		ID, Generation, Owner, Workspace, Tree string
		State                                  State
		Snapshot                               worktree.Snapshot
		Candidate                              worktree.Candidate
		Content                                evidence.ContentSnapshot
	}{e.ID, e.RunGeneration, e.ResumeOwner, e.Workspace, e.Tree, e.State, e.Snap, e.Cand, content})
	if err != nil {
		return preview, content, err
	}
	digest := sha256.Sum256(raw)
	preview.Revision, preview.Entries = hex.EncodeToString(digest[:]), content.Entries
	return preview, content, nil
}

func discardContentUnchanged(ctx context.Context, e *Entry) bool {
	return e.DiscardContent != nil && sameContent(*e.DiscardContent, evidence.CaptureContentSnapshot(ctx, e.Cand.WorktreeRoot, 50_000))
}

func supportedDiscardContent(d durableEntry) bool {
	if d.DiscardApproval != "" {
		if d.DiscardContent == nil || d.State != Discarded || len(d.DiscardApproval) != sha256.Size*2 {
			return false
		}
		if _, err := hex.DecodeString(d.DiscardApproval); err != nil {
			return false
		}
	}
	if d.DiscardContent == nil {
		return true
	}
	content := *d.DiscardContent
	if d.State != Discarded {
		return false
	}
	if content == (evidence.ContentSnapshot{}) {
		return true
	}
	if !content.Complete || content.Scope != "workspace-content-v1" || len(content.Digest) != sha256.Size*2 || content.Reason != "" {
		return false
	}
	_, err := hex.DecodeString(content.Digest)
	return err == nil
}

func candidateAbsent(e *Entry) error {
	_, err := os.Lstat(e.Cand.WorktreeRoot)
	return err
}

// DiscardExpected is the host UI path for an explicit, previewed discard.
func (s *Store) DiscardExpected(ctx context.Context, id, revision string) (*Entry, error) {
	if len(revision) != sha256.Size*2 {
		return nil, tool.Refusal{Code: CodeStaleDiscard, Message: "a current discard preview is required"}
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return nil, tool.Refusal{Code: CodeStaleDiscard, Message: "invalid discard preview revision"}
	}
	return s.discard(ctx, id, revision)
}

func (s *Store) DiscardResultExpected(ctx context.Context, id, revision string) error {
	if strings.HasPrefix(id, "int_") {
		store, err := s.managementIntegrationStore(ctx, id)
		if err != nil {
			return err
		}
		return store.discardIntegrationExpected(ctx, id, revision)
	}
	store, err := s.managementStore(ctx, id)
	if err != nil {
		return err
	}
	_, err = store.DiscardExpected(ctx, id, revision)
	return err
}
