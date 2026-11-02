package memory

import "errors"

// ErrPinnedReview refuses an automatic change to user-pinned guidance.
var ErrPinnedReview = errors.New("automatic memory review cannot change pinned guidance")

// ArchiveAt archives the observed revision only, preserving user-pinned facts.
func (s Store) ArchiveAt(name string, revision int) (string, error) {
	memoryStoreMutationMu.Lock()
	defer memoryStoreMutationMu.Unlock()
	release, err := s.lockMutations()
	if err != nil {
		return "", err
	}
	defer release()
	active, _, exists, err := s.findActive(name)
	if err != nil {
		return "", err
	}
	actual := 0
	if exists {
		actual = active.Revision
	}
	if actual != revision {
		return "", memoryRevisionConflict(revision, actual)
	}
	if ResolveActivation(active) == ActivationPinned {
		return "", ErrPinnedReview
	}
	return s.archiveLocked(name)
}
