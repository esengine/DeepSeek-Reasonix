package delegation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/state/sessionstore"
)

type EnvironmentSourceKind uint8

const (
	EnvironmentResume EnvironmentSourceKind = iota
	EnvironmentCopy
)

// EnvironmentSource is resolved by the host from a persisted transcript. It is
// not a tool argument and never grants additional file or model capabilities.
type EnvironmentSource struct {
	Kind            EnvironmentSourceKind
	Reference       string
	ID              string
	Generation      string
	Owner           string
	WorkspaceRoot   string
	OriginWorkspace string
	reservation     *continuationReservation
}

type continuationReservation struct{ release atomic.Pointer[func()] }

func (r *continuationReservation) close() {
	if r != nil {
		if release := r.release.Swap(nil); release != nil {
			(*release)()
		}
	}
}

func (s *EnvironmentSource) release() {
	if s != nil {
		s.reservation.close()
	}
}

func (s *EnvironmentSource) active() bool {
	return s != nil && s.reservation != nil && s.reservation.release.Load() != nil
}

func sourceIdentity(meta sessionstore.SubagentMeta) *EnvironmentSource {
	return &EnvironmentSource{Reference: meta.Ref, ID: meta.EnvironmentID, Generation: meta.EnvironmentGeneration,
		Owner: meta.ParentSession, WorkspaceRoot: meta.WorkspaceRoot, OriginWorkspace: meta.OriginWorkspace}
}

func (s *SubagentStore) continuationSource(ctx context.Context, ref, parent string) (result *EnvironmentSource, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	meta, err := s.LoadMeta(ref)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(meta.ParentSession) == parent {
		return sourceIdentity(meta), nil
	}
	if err := s.validateForkOwner(meta, SubagentSpec{ParentSession: parent}); err != nil {
		return nil, err
	}
	nearest, err := s.ancestorReference(ref, parent)
	if err != nil {
		return nil, err
	}
	reservation, err := s.reserveContinuation(parent, nearest)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil || result == nil || result.Kind != EnvironmentCopy {
			reservation.close()
		}
	}()
	copies, err := s.currentContinuationCopies(ref, parent)
	if err != nil {
		return nil, err
	}
	if len(copies) > 1 {
		return nil, fmt.Errorf("subagent reference %q has multiple copied transcripts in current parent session %q; use an exact copied reference", ref, parent)
	}
	if len(copies) == 1 {
		return sourceIdentity(copies[0].Meta), nil
	}
	meta, err = s.LoadMeta(nearest)
	if err != nil {
		return nil, err
	}
	result = sourceIdentity(meta)
	result.Kind, result.reservation = EnvironmentCopy, reservation
	return result, nil
}

func (s *SubagentStore) reserveContinuation(parent, ref string) (*continuationReservation, error) {
	key := sha256.Sum256([]byte(parent + "\x00" + ref))
	dir := filepath.Join(s.dir, ".continuations-v1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	release, err := filelock.TryAcquire(filepath.Join(dir, hex.EncodeToString(key[:])+".lock"))
	if errors.Is(err, filelock.ErrHeld) {
		return nil, tool.Refusal{Code: isolation.CodeBusy, Message: "another call is preparing this conversation's continuation copy; retry after preparation finishes"}
	}
	if err != nil {
		return nil, err
	}
	r := &continuationReservation{}
	r.release.Store(&release)
	return r, nil
}

func (s *SubagentStore) currentContinuationCopies(ref, parent string) ([]sessionstore.SubagentArtifact, error) {
	artifacts, err := sessionstore.ListSubagentsByParent(filepath.Dir(s.dir), parent)
	if err != nil {
		return nil, err
	}
	var copies []sessionstore.SubagentArtifact
	for _, artifact := range artifacts {
		if artifact.Meta.ForkedFrom == ref || s.derivesFrom(artifact.Meta, ref) {
			copies = append(copies, artifact)
		}
	}
	return copies, nil
}

func (s *SubagentStore) ancestorReference(ref, parent string) (string, error) {
	ancestors, err := s.sessionAncestors(parent)
	if err != nil {
		return "", err
	}
	for _, ancestor := range ancestors {
		artifacts, err := sessionstore.ListSubagentsByParent(filepath.Dir(s.dir), ancestor)
		if err != nil {
			return "", err
		}
		var candidates []string
		for _, artifact := range artifacts {
			if artifact.Ref == ref || s.derivesFrom(artifact.Meta, ref) {
				candidates = append(candidates, artifact.Ref)
			}
		}
		if len(candidates) > 1 {
			return "", fmt.Errorf("subagent reference %q has multiple candidate transcripts in ancestor parent session %q", ref, ancestor)
		}
		if len(candidates) == 1 {
			return candidates[0], nil
		}
	}
	return "", fmt.Errorf("subagent reference %q is not in current parent session %q lineage", ref, parent)
}

func continuationSourceSpec(meta sessionstore.SubagentMeta, spec SubagentSpec) (SubagentSpec, error) {
	source := spec.continuationSource
	if source == nil {
		return spec, nil
	}
	if !source.active() || source.Kind != EnvironmentCopy || source.Reference != meta.Ref ||
		source.ID != meta.EnvironmentID || source.Generation != meta.EnvironmentGeneration || source.Owner != meta.ParentSession ||
		source.WorkspaceRoot != meta.WorkspaceRoot || source.OriginWorkspace != meta.OriginWorkspace {
		return spec, fmt.Errorf("subagent continuation source changed during environment preparation; retry from its current reference")
	}
	spec.EnvironmentID, spec.EnvironmentGeneration = source.ID, source.Generation
	spec.WorkspaceRoot, spec.OriginWorkspace = source.WorkspaceRoot, source.OriginWorkspace
	return spec, nil
}
