package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
)

func (s *Store) openManagedIntegration(ctx context.Context, id string) (*Integration, error) {
	if s == nil || s.mutations == nil {
		return nil, errUnavailable
	}
	attempt, _, err := s.readIntegration(id)
	if err != nil {
		return nil, err
	}
	original, err := s.managedResultMetadata(attempt.ResultID)
	if err != nil {
		return nil, err
	}
	if original.ResumeOwner != attempt.Owner {
		return nil, tool.Refusal{Code: CodeOwnerMismatch, Message: "integration original identity does not belong to this workspace"}
	}
	i, err := s.openIntegration(ctx, id, attempt.Owner, false)
	if errors.Is(err, filelock.ErrHeld) {
		return nil, tool.Refusal{Code: CodeBusy, Message: "integration attempt is still active"}
	}
	return i, err
}

func (s *Store) previewIntegrationDiscard(ctx context.Context, id string) (DiscardPreview, error) {
	i, err := s.openManagedIntegration(ctx, id)
	if err != nil {
		return DiscardPreview{}, err
	}
	defer i.Close()
	lease, err := i.holdCandidate(ctx, "isolation.integration.discard_preview")
	if err != nil {
		return DiscardPreview{}, err
	}
	defer lease.End(mutation.NotExecuted)
	preview, _, err := i.inspectDiscard(ctx)
	return preview, err
}

func (s *Store) discardIntegrationExpected(ctx context.Context, id, revision string) error {
	if len(revision) != sha256.Size*2 {
		return tool.Refusal{Code: CodeStaleDiscard, Message: "a current integration discard preview is required"}
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return tool.Refusal{Code: CodeStaleDiscard, Message: "invalid discard preview revision"}
	}
	i, err := s.openManagedIntegration(ctx, id)
	if err != nil {
		return err
	}
	defer i.Close()
	return i.discard(ctx, revision)
}

func (i *Integration) inspectDiscard(ctx context.Context) (DiscardPreview, evidence.ContentSnapshot, error) {
	a := i.attempt
	if a.State == Applying || a.State == Integrated {
		return DiscardPreview{}, evidence.ContentSnapshot{}, tool.Refusal{Code: CodeRecoveryRequired, Message: "settle integration recovery before discarding this attempt"}
	}
	content := evidence.CaptureContentSnapshot(ctx, a.Candidate.WorktreeRoot, 50_000)
	if !content.Complete {
		if _, err := os.Lstat(a.Candidate.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
			return DiscardPreview{}, content, tool.Refusal{Code: CodeRecoveryRequired, Message: "integration candidate contents are not completely observed; files retained"}
		}
		content = evidence.ContentSnapshot{}
	}
	raw, err := json.Marshal(struct {
		Attempt IntegrationAttempt
		Content evidence.ContentSnapshot
	}{a, content})
	if err != nil {
		return DiscardPreview{}, content, err
	}
	digest := sha256.Sum256(raw)
	return DiscardPreview{ID: a.ID, Revision: hex.EncodeToString(digest[:]), CandidateRoot: a.Candidate.WorktreeRoot, Entries: content.Entries, State: a.State}, content, nil
}
