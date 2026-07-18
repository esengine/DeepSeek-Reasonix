// Package snapshotowner composes immutable Remote history retention with the
// contentRef store at the final wire-owner budget boundary. It deliberately
// owns no transport, subscription registry, Session actor, or lease lifecycle.
package snapshotowner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/remote/contentref"
	"reasonix/internal/remote/history"
	"reasonix/internal/remote/protocol"
)

var (
	// ErrNilStore reports a construction or method call without both daemon
	// epoch stores. A snapshot owner cannot safely degrade to unretained data.
	ErrNilStore = errors.New("snapshotowner: history and contentref stores are required")
	// ErrBindingMismatch reports disagreement between the outer snapshot and
	// immutable history provenance supplied for a new subscription.
	ErrBindingMismatch = errors.New("snapshotowner: snapshot and history bindings do not match")
)

// Builder performs transactional snapshot construction over daemon-owned
// history and content stores. The stores outlive Builder and remain owned by
// the daemon; Builder never closes them.
type Builder struct {
	histories *history.Store
	contents  *contentref.Store
}

// New creates a final-owner builder. Both stores are mandatory and must belong
// to the same daemon epoch used by every snapshot binding.
func New(histories *history.Store, contents *contentref.Store) (*Builder, error) {
	if histories == nil || contents == nil {
		return nil, ErrNilStore
	}
	return &Builder{histories: histories, contents: contents}, nil
}

// BuildSubscribeSnapshot publishes capture and selects the largest newest
// complete-turn page that fits after insertion into the complete outer
// SessionSnapshot. History is never externalized as a nested owner first.
//
// On failure after publication, both history retention and all content owned
// by snapshotId are released. On success they remain retained for the active
// subscription until Release is called.
func (m *Builder) BuildSubscribeSnapshot(
	base protocol.SessionSnapshot,
	capture history.Capture,
	pageTurns int,
	leaseID protocol.LeaseID,
) (protocol.SessionSnapshot, error) {
	if err := m.validateStores(); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if err := validatePageTurns(pageTurns); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if err := validateLease(leaseID); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	if err := validateSubscribeBinding(base, capture.Binding, m.contents.HostEpoch()); err != nil {
		return protocol.SessionSnapshot{}, err
	}

	if err := m.histories.CaptureSnapshot(capture); err != nil {
		return protocol.SessionSnapshot{}, err
	}
	retained := false
	defer func() {
		if retained {
			return
		}
		m.release(capture.Binding)
	}()

	// A snapshotId is unique within the daemon epoch. If a prior, already
	// invalid history owner leaked content before its ID was (incorrectly)
	// reused, discard it only after this new capture has won publication. A
	// duplicate live capture fails above and therefore cannot lose its owner.
	owner, err := contentref.SnapshotOwner(capture.Binding.SnapshotID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	m.contents.ReleaseOwner(owner)

	var finalized protocol.SessionSnapshot
	accept := func(candidate protocol.HistoryPage) (bool, error) {
		attempt := base
		attempt.History = candidate
		result, externalizeErr := contentref.ExternalizeSessionSnapshot(
			m.contents,
			attempt,
			contentref.ExternalizeOptions{LeaseID: leaseID},
		)
		if errors.Is(externalizeErr, contentref.ErrOwnerBudget) {
			return false, nil
		}
		if externalizeErr != nil {
			return false, externalizeErr
		}
		cloned, cloneErr := cloneJSON(result)
		if cloneErr != nil {
			releaseDescriptors(m.contents, result.Externalized)
			return false, cloneErr
		}
		finalized = cloned
		return true, nil
	}
	if _, err := m.histories.LatestFitting(capture.Binding, pageTurns, accept); err != nil {
		return protocol.SessionSnapshot{}, normalizeHistoryUnavailable(err, capture.Binding.Target)
	}
	retained = true
	return finalized, nil
}

// BuildOlderHistory builds one snapshot-bound older page under the same
// content owner and lease provenance as the active subscription. Only a final
// ErrOwnerBudget retries with fewer whole turns; capacity and all other errors
// terminate immediately.
func (m *Builder) BuildOlderHistory(
	binding history.Binding,
	cursor protocol.Cursor,
	pageTurns int,
	leaseID protocol.LeaseID,
) (protocol.HistoryPage, error) {
	if err := m.validateStores(); err != nil {
		return protocol.HistoryPage{}, err
	}
	if err := validatePageTurns(pageTurns); err != nil {
		return protocol.HistoryPage{}, err
	}
	if err := validateLease(leaseID); err != nil {
		return protocol.HistoryPage{}, err
	}
	if err := validateBinding(binding); err != nil {
		return protocol.HistoryPage{}, err
	}
	if strings.TrimSpace(string(cursor)) == "" {
		return protocol.HistoryPage{}, fmt.Errorf("%w: cursor is empty", history.ErrInvalidCapture)
	}
	if m.contents.HostEpoch() != binding.HostEpoch {
		return protocol.HistoryPage{}, contentref.ErrEpochMismatch
	}

	var finalized protocol.HistoryPage
	accept := func(candidate protocol.HistoryPage) (bool, error) {
		result, externalizeErr := contentref.ExternalizeHistoryPage(
			m.contents,
			candidate,
			contentref.ExternalizeOptions{
				LeaseID:      leaseID,
				Target:       binding.Target,
				RuntimeEpoch: binding.RuntimeEpoch,
			},
		)
		if errors.Is(externalizeErr, contentref.ErrOwnerBudget) {
			return false, nil
		}
		if externalizeErr != nil {
			return false, externalizeErr
		}
		cloned, cloneErr := cloneJSON(result)
		if cloneErr != nil {
			releaseDescriptors(m.contents, result.Externalized)
			return false, cloneErr
		}
		finalized = cloned
		return true, nil
	}
	if _, err := m.histories.OlderFitting(binding, cursor, pageTurns, accept); err != nil {
		return protocol.HistoryPage{}, normalizeHistoryUnavailable(err, binding.Target)
	}
	return finalized, nil
}

// Release invalidates retained history, every cursor, and every contentRef
// owned by binding.SnapshotID. Callers carry the complete trusted binding from
// the active subscription; no partial identity is accepted. This is a
// lifecycle primitive, not an authorization check: adapters must never pass
// client-supplied identity before validating it against the active binding.
func (m *Builder) Release(binding history.Binding) (historyReleased bool, contentRefsReleased int) {
	if m == nil || m.histories == nil || m.contents == nil || validateBinding(binding) != nil ||
		binding.HostEpoch != m.contents.HostEpoch() {
		return false, 0
	}
	return m.release(binding)
}

func (m *Builder) release(binding history.Binding) (bool, int) {
	historyReleased := m.histories.Release(binding)
	owner, err := contentref.SnapshotOwner(binding.SnapshotID)
	if err != nil {
		return historyReleased, 0
	}
	return historyReleased, m.contents.ReleaseOwner(owner)
}

func (m *Builder) validateStores() error {
	if m == nil || m.histories == nil || m.contents == nil {
		return ErrNilStore
	}
	return nil
}

func validateSubscribeBinding(base protocol.SessionSnapshot, binding history.Binding, contentHostEpoch protocol.HostEpoch) error {
	if err := validateBinding(binding); err != nil {
		return err
	}
	if strings.TrimSpace(string(base.SnapshotID)) == "" || strings.TrimSpace(string(base.HostEpoch)) == "" ||
		strings.TrimSpace(string(base.RuntimeEpoch)) == "" || base.Target.Validate() != nil {
		return fmt.Errorf("%w: outer snapshot identity is incomplete", ErrBindingMismatch)
	}
	if base.SnapshotID != binding.SnapshotID || base.HostEpoch != binding.HostEpoch ||
		base.Target != binding.Target || base.RuntimeEpoch != binding.RuntimeEpoch ||
		base.History.SnapshotID != binding.SnapshotID {
		return ErrBindingMismatch
	}
	if contentHostEpoch != binding.HostEpoch {
		return contentref.ErrEpochMismatch
	}
	if len(base.Externalized) != 0 || len(base.History.Externalized) != 0 {
		return contentref.ErrAlreadyExternalized
	}
	if err := base.Validate(); err != nil {
		return fmt.Errorf("snapshotowner: invalid base SessionSnapshot: %w", err)
	}
	return nil
}

func validateBinding(binding history.Binding) error {
	if strings.TrimSpace(string(binding.SnapshotID)) == "" || strings.TrimSpace(string(binding.HostEpoch)) == "" ||
		strings.TrimSpace(string(binding.RuntimeEpoch)) == "" {
		return fmt.Errorf("%w: history identity is incomplete", history.ErrInvalidCapture)
	}
	if err := binding.Target.Validate(); err != nil {
		return fmt.Errorf("%w: history target: %v", history.ErrInvalidCapture, err)
	}
	return nil
}

func validateLease(leaseID protocol.LeaseID) error {
	if strings.TrimSpace(string(leaseID)) == "" {
		return contentref.ErrInvalidLease
	}
	return nil
}

func validatePageTurns(pageTurns int) error {
	if pageTurns < 1 || pageTurns > protocol.HistoryMaxTurns {
		return history.ErrInvalidPageTurns
	}
	return nil
}

func normalizeHistoryUnavailable(err error, target protocol.RuntimeTarget) error {
	if errors.Is(err, history.ErrClosed) {
		targetCopy := target
		return protocol.MustRemoteError(protocol.ErrSnapshotExpired, protocol.ErrorOptions{Target: &targetCopy})
	}
	return err
}

func releaseDescriptors(store *contentref.Store, descriptors []protocol.ExternalizedField) {
	for _, descriptor := range descriptors {
		store.Release(descriptor.ContentRef)
	}
}

func cloneJSON[T any](value T) (T, error) {
	var out T
	body, err := json.Marshal(value)
	if err != nil {
		return out, fmt.Errorf("snapshotowner: clone owner: %w", err)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("snapshotowner: clone owner: %w", err)
	}
	return out, nil
}
