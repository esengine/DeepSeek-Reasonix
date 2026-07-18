package history

import (
	"errors"

	"reasonix/internal/remote/contentref"
	"reasonix/internal/remote/protocol"
)

// LatestBudgeted returns the newest page after contentRef externalization and
// exact frozen 2 MiB JSON budgeting. If the requested page does not fit, it is
// retried with one fewer complete logical turn until the largest fitting suffix
// is found. A single turn is never split or silently omitted.
func (s *Store) LatestBudgeted(
	binding Binding,
	pageTurns int,
	contents *contentref.Store,
	options contentref.ExternalizeOptions,
) (protocol.HistoryPage, error) {
	return s.budgeted(binding, "", pageTurns, true, contents, options)
}

// OlderBudgeted is LatestBudgeted for a snapshot-bound older-page cursor.
func (s *Store) OlderBudgeted(
	binding Binding,
	cursor protocol.Cursor,
	pageTurns int,
	contents *contentref.Store,
	options contentref.ExternalizeOptions,
) (protocol.HistoryPage, error) {
	return s.budgeted(binding, cursor, pageTurns, false, contents, options)
}

func (s *Store) budgeted(
	binding Binding,
	cursor protocol.Cursor,
	pageTurns int,
	latest bool,
	contents *contentref.Store,
	options contentref.ExternalizeOptions,
) (protocol.HistoryPage, error) {
	if contents == nil {
		return protocol.HistoryPage{}, contentref.ErrClosed
	}
	if contents.HostEpoch() != binding.HostEpoch {
		return protocol.HistoryPage{}, contentref.ErrEpochMismatch
	}
	// Provenance is authoritative in the immutable history binding, not in
	// adapter-supplied options. LeaseID and explicit pointer choices remain the
	// caller's transport policy.
	options.Target = binding.Target
	options.RuntimeEpoch = binding.RuntimeEpoch
	var finalized protocol.HistoryPage
	accept := func(candidate protocol.HistoryPage) (bool, error) {
		page, err := contentref.ExternalizeHistoryPage(contents, candidate, options)
		if errors.Is(err, contentref.ErrOwnerBudget) || errors.Is(err, contentref.ErrCapacity) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		finalized = page
		return true, nil
	}
	var err error
	if latest {
		_, err = s.LatestFitting(binding, pageTurns, accept)
	} else {
		_, err = s.OlderFitting(binding, cursor, pageTurns, accept)
	}
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	return finalized, nil
}
