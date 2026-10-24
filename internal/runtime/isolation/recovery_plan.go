package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
)

type recoveryPlan struct {
	preview   worktree.RecoveryPreview
	journal   *worktree.RecoveryPreview
	originals []*Entry
	attempts  []IntegrationAttempt
}

func (s *Store) PreviewRecovery(ctx context.Context) (*worktree.RecoveryPreview, error) {
	plan, err := s.recoveryPlan(ctx)
	if err != nil || plan == nil {
		return nil, err
	}
	return &plan.preview, nil
}

func (s *Store) recoveryPlan(ctx context.Context) (*recoveryPlan, error) {
	if s == nil || !s.repo.Valid() {
		return nil, nil
	}
	journal, err := worktree.PreviewRecovery(s.repo)
	if err != nil {
		return nil, err
	}
	plan := &recoveryPlan{journal: journal}
	identities := map[string]string{}
	originals := map[string]bool{}
	records, err := s.resultRecords()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		d := record.entry
		if d.State == Applying && sameDir(d.Workspace, s.workspace) {
			originals[d.ID] = true
		}
	}
	paths, err := s.integrationRecordPaths()
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(filepath.Base(path), ".json")
		attempt, raw, err := s.readIntegration(id)
		if err != nil {
			if attempt.Source.RepoRoot != "" && !sameDir(attempt.Source.RepoRoot, s.repo.WorkTree) {
				continue
			}
			return nil, err
		}
		if attempt.State != Applying {
			continue
		}
		if _, err := s.managedResultMetadata(attempt.ResultID); err != nil {
			var refusal tool.Refusal
			if errors.As(err, &refusal) && refusal.Code == CodeOwnerMismatch {
				continue
			}
			return nil, err
		}
		originals[attempt.ResultID] = true
		identities[id] = rawRecoveryDigest(raw)
		plan.attempts = append(plan.attempts, attempt)
	}
	ids := make([]string, 0, len(originals))
	for id := range originals {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		d, err := s.managedResultMetadata(id)
		if err != nil {
			return nil, err
		}
		e := d.entry()
		raw, err := os.ReadFile(entryRecordPath(e))
		if err != nil {
			return nil, err
		}
		identities[id] = rawRecoveryDigest(raw)
		plan.originals = append(plan.originals, e)
		paths, err := worktree.ChangePaths(e.Snap, e.Changes)
		if err != nil {
			return nil, err
		}
		plan.preview.Paths = append(plan.preview.Paths, paths...)
	}
	if journal == nil && len(identities) == 0 {
		return nil, nil
	}
	if journal != nil {
		identities["journal"] = journal.Revision
		plan.preview.Paths = append(plan.preview.Paths, journal.Paths...)
	}
	slices.Sort(plan.preview.Paths)
	plan.preview.Paths = slices.Compact(plan.preview.Paths)
	if plan.preview.Paths == nil {
		plan.preview.Paths = []string{}
	}
	raw, err := json.Marshal(struct {
		Workspace string
		Records   map[string]string
	}{s.workspace, identities})
	if err != nil {
		return nil, err
	}
	plan.preview.Revision = rawRecoveryDigest(raw)
	return plan, nil
}

func rawRecoveryDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
