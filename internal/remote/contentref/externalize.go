package contentref

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"reasonix/internal/remote/protocol"
)

const ContentObjectLimitReason = "object_limit"

const (
	// rpcwire writes this fixed JSON-RPC notification envelope around a
	// SessionEvent and includes the trailing NDJSON newline in its frame limit.
	sessionEventFrameOverhead = len(`{"jsonrpc":"2.0","method":"","params":}`) + len(protocol.MethodSessionEvent) + len("\n")
	sessionEventPayloadBytes  = protocol.FrameBytes - sessionEventFrameOverhead
)

var (
	ErrAlreadyExternalized = errors.New("contentref: owner already contains externalized descriptors")
	ErrUnknownPointer      = errors.New("contentref: requested pointer is absent or not schema-externalizable")
	ErrDuplicatePointer    = errors.New("contentref: requested externalization pointers must be unique")
	ErrOwnerBudget         = errors.New("contentref: owner cannot fit its frozen wire budget")
)

// ExternalizeOptions lets an owner builder select known semantic fields before
// automatic budget reduction. Every field over 64 KiB is externalized
// regardless. If the wire owner still exceeds its frozen budget, the remaining
// tagged fields are selected by largest encoded saving until it fits.
type ExternalizeOptions struct {
	AdditionalJSONPointers []string
	// LeaseID binds issued references to the daemon lease that produced the
	// snapshot/history/event. The zero value is reserved for package-level use;
	// daemon session/content handlers use Store.ReadForLease.
	LeaseID protocol.LeaseID
	// Target and RuntimeEpoch supply provenance for a standalone HistoryPage;
	// they are required whenever LeaseID is set. SessionSnapshot and SessionEvent
	// derive them from the owner.
	Target       protocol.RuntimeTarget
	RuntimeEpoch protocol.RuntimeEpoch
}

// ExternalizeSessionEvent externalizes only schema-tagged strings in the
// complete SessionEvent envelope. Its owner identity is runtimeEpoch + seq,
// and its final payload reserves the fixed JSON-RPC notification envelope so
// the complete NDJSON frame stays within 8 MiB. It does not use the stricter
// snapshot/history budget.
func ExternalizeSessionEvent(store *Store, event protocol.SessionEvent, options ExternalizeOptions) (protocol.SessionEvent, error) {
	if store == nil {
		return protocol.SessionEvent{}, ErrClosed
	}
	if len(event.Externalized) != 0 {
		return protocol.SessionEvent{}, ErrAlreadyExternalized
	}
	if event.HostEpoch != store.hostEpoch {
		return protocol.SessionEvent{}, ErrEpochMismatch
	}
	if err := event.Validate(); err != nil {
		return protocol.SessionEvent{}, fmt.Errorf("contentref: invalid SessionEvent: %w", err)
	}
	owner, err := EventOwner(event.RuntimeEpoch, event.Seq)
	if err != nil {
		return protocol.SessionEvent{}, err
	}
	binding := ReferenceBinding{Target: event.Target, RuntimeEpoch: event.RuntimeEpoch}
	return externalizeOwner(store, owner, binding, event, options, sessionEventPayloadBytes, func(value *protocol.SessionEvent, fields []protocol.ExternalizedField) {
		value.Externalized = fields
	})
}

// ExternalizeHistoryPage binds all references to the page snapshotId and
// enforces the frozen 2 MiB page budget after null placeholders/descriptors.
func ExternalizeHistoryPage(store *Store, page protocol.HistoryPage, options ExternalizeOptions) (protocol.HistoryPage, error) {
	if store == nil {
		return protocol.HistoryPage{}, ErrClosed
	}
	if len(page.Externalized) != 0 {
		return protocol.HistoryPage{}, ErrAlreadyExternalized
	}
	if err := page.Validate(); err != nil {
		return protocol.HistoryPage{}, fmt.Errorf("contentref: invalid HistoryPage: %w", err)
	}
	owner, err := SnapshotOwner(page.SnapshotID)
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	binding := ReferenceBinding{Target: options.Target, RuntimeEpoch: options.RuntimeEpoch}
	return externalizeOwner(store, owner, binding, page, options, protocol.SnapshotHistoryBytes, func(value *protocol.HistoryPage, fields []protocol.ExternalizedField) {
		value.Externalized = fields
	})
}

// ExternalizeSessionSnapshot binds all references, including nested history and
// live-event fields, to the outer snapshotId and enforces the frozen 2 MiB
// snapshot budget. A nested HistoryPage must not have been externalized
// separately.
func ExternalizeSessionSnapshot(store *Store, snapshot protocol.SessionSnapshot, options ExternalizeOptions) (protocol.SessionSnapshot, error) {
	if store == nil {
		return protocol.SessionSnapshot{}, ErrClosed
	}
	if len(snapshot.Externalized) != 0 || len(snapshot.History.Externalized) != 0 {
		return protocol.SessionSnapshot{}, ErrAlreadyExternalized
	}
	if snapshot.HostEpoch != store.hostEpoch {
		return protocol.SessionSnapshot{}, ErrEpochMismatch
	}
	if err := snapshot.Validate(); err != nil {
		return protocol.SessionSnapshot{}, fmt.Errorf("contentref: invalid SessionSnapshot: %w", err)
	}
	// The nested HistoryPage is part of the snapshot owner, so its own
	// descriptor list stays empty. Keep the required wire array non-null.
	snapshot.History.Externalized = []protocol.ExternalizedField{}
	owner, err := SnapshotOwner(snapshot.SnapshotID)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	binding := ReferenceBinding{Target: snapshot.Target, RuntimeEpoch: snapshot.RuntimeEpoch}
	return externalizeOwner(store, owner, binding, snapshot, options, protocol.SnapshotHistoryBytes, func(value *protocol.SessionSnapshot, fields []protocol.ExternalizedField) {
		value.Externalized = fields
	})
}

type candidate struct {
	pointer       string
	value         string
	wireValueSize int
}

func externalizeOwner[T any](
	store *Store,
	owner Owner,
	binding ReferenceBinding,
	value T,
	options ExternalizeOptions,
	wireBudget int,
	setFields func(*T, []protocol.ExternalizedField),
) (T, error) {
	var zero T
	discovered, err := protocol.ExternalizableStrings(value)
	if err != nil {
		return zero, err
	}
	candidates := make(map[string]candidate, len(discovered))
	for _, field := range discovered {
		if !utf8.ValidString(field.Value) {
			return zero, fmt.Errorf("%w: %s", ErrInvalidUTF8, field.JSONPointer)
		}
		encoded, err := json.Marshal(field.Value)
		if err != nil {
			return zero, err
		}
		candidates[field.JSONPointer] = candidate{
			pointer:       field.JSONPointer,
			value:         field.Value,
			wireValueSize: len(encoded),
		}
	}

	selected := make(map[string]bool)
	for pointer, field := range candidates {
		if len([]byte(field.value)) > protocol.ExternalizeFieldBytes {
			selected[pointer] = true
		}
	}
	requested := make(map[string]bool, len(options.AdditionalJSONPointers))
	for _, pointer := range options.AdditionalJSONPointers {
		if requested[pointer] {
			return zero, fmt.Errorf("%w: %s", ErrDuplicatePointer, pointer)
		}
		requested[pointer] = true
		if _, ok := candidates[pointer]; !ok {
			return zero, fmt.Errorf("%w: %s", ErrUnknownPointer, pointer)
		}
		selected[pointer] = true
	}

	prepared, err := prepareSelected(candidates, selected)
	if err != nil {
		return zero, err
	}
	planned := plannedDescriptors(prepared)
	testValue := value
	setFields(&testValue, planned)
	wire, err := json.Marshal(testValue)
	if err != nil {
		return zero, err
	}

	if len(wire) > wireBudget {
		remaining := make([]candidate, 0, len(candidates)-len(selected))
		for pointer, field := range candidates {
			if !selected[pointer] {
				remaining = append(remaining, field)
			}
		}
		sort.Slice(remaining, func(i, j int) bool {
			if remaining[i].wireValueSize == remaining[j].wireValueSize {
				return remaining[i].pointer < remaining[j].pointer
			}
			return remaining[i].wireValueSize > remaining[j].wireValueSize
		})
		currentSize := len(wire)
		for _, field := range remaining {
			selected[field.pointer] = true
			attemptPrepared, prepareErr := prepareSelected(candidates, selected)
			if prepareErr != nil {
				return zero, prepareErr
			}
			attempt := value
			setFields(&attempt, plannedDescriptors(attemptPrepared))
			attemptWire, marshalErr := json.Marshal(attempt)
			if marshalErr != nil {
				return zero, marshalErr
			}
			if len(attemptWire) >= currentSize {
				delete(selected, field.pointer)
				continue
			}
			prepared = attemptPrepared
			wire = attemptWire
			currentSize = len(attemptWire)
			if currentSize <= wireBudget {
				break
			}
		}
	}
	if len(wire) > wireBudget {
		return zero, ErrOwnerBudget
	}
	if len(prepared) == 0 {
		// Return the exact value used for the budget check. Besides avoiding a
		// nil-vs-empty size discrepancy at the boundary, this guarantees the
		// required externalized field is encoded as [] rather than JSON null.
		return testValue, nil
	}

	descriptors, err := store.putBatch(owner, options.LeaseID, binding, prepared)
	if err != nil {
		return zero, err
	}
	out := value
	setFields(&out, descriptors)
	finalWire, err := json.Marshal(out)
	if err != nil || len(finalWire) > wireBudget {
		for _, descriptor := range descriptors {
			store.Release(descriptor.ContentRef)
		}
		if err != nil {
			return zero, err
		}
		return zero, ErrOwnerBudget
	}
	return out, nil
}

func prepareSelected(candidates map[string]candidate, selected map[string]bool) ([]storedField, error) {
	pointers := make([]string, 0, len(selected))
	for pointer := range selected {
		pointers = append(pointers, pointer)
	}
	sort.Strings(pointers)
	fields := make([]storedField, 0, len(pointers))
	for _, pointer := range pointers {
		field := candidates[pointer]
		data := []byte(field.value)
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("%w: %s", ErrInvalidUTF8, pointer)
		}
		stored := storedField{pointer: pointer, data: append([]byte(nil), data...)}
		if len(data) > protocol.ContentRefObjectBytes {
			stored.data = truncateHeadTail(data, protocol.ContentRefObjectBytes)
			stored.originalBytes = int64(len(data))
			stored.truncated = true
			stored.truncationNote = ContentObjectLimitReason
		}
		fields = append(fields, stored)
	}
	return fields, nil
}

func plannedDescriptors(fields []storedField) []protocol.ExternalizedField {
	descriptors := make([]protocol.ExternalizedField, len(fields))
	placeholder := protocol.ContentRef(contentRefPrefix + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	for i, field := range fields {
		digest := sha256.Sum256(field.data)
		descriptor := protocol.ExternalizedField{
			JSONPointer: field.pointer,
			ContentRef:  placeholder,
			TotalBytes:  int64(len(field.data)),
			SHA256:      hex.EncodeToString(digest[:]),
			Truncated:   field.truncated,
		}
		if field.truncated {
			original := field.originalBytes
			descriptor.OriginalBytes = &original
			descriptor.TruncationReason = field.truncationNote
		}
		descriptors[i] = descriptor
	}
	return descriptors
}

func truncateHeadTail(data []byte, limit int) []byte {
	if len(data) <= limit {
		return append([]byte(nil), data...)
	}
	headEnd := limit / 2
	for headEnd > 0 && !utf8.RuneStart(data[headEnd]) {
		headEnd--
	}
	tailBudget := limit - headEnd
	tailStart := len(data) - tailBudget
	for tailStart < len(data) && !utf8.RuneStart(data[tailStart]) {
		tailStart++
	}
	out := make([]byte, 0, headEnd+len(data)-tailStart)
	out = append(out, data[:headEnd]...)
	out = append(out, data[tailStart:]...)
	return out
}
