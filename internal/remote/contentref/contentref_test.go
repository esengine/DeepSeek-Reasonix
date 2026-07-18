package contentref

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/eventwire"
	"reasonix/internal/remote/protocol"
)

const testHostEpoch protocol.HostEpoch = "host-epoch-test"

func newTestStore(t *testing.T, config Config) *Store {
	t.Helper()
	store, err := New(testHostEpoch, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func testEvent(seq uint64, text string) protocol.SessionEvent {
	return protocol.SessionEvent{
		SubscriptionID: "subscription-test",
		HostEpoch:      testHostEpoch,
		Target: protocol.RuntimeTarget{
			WorkspaceID: "workspace-test",
			SessionID:   "session-test",
		},
		RuntimeEpoch: "runtime-test",
		Seq:          seq,
		Event:        eventwire.Event{Kind: "text", Text: text},
	}
}

func testRef(char byte) protocol.ContentRef {
	return protocol.ContentRef(contentRefPrefix + strings.Repeat(string(char), contentRefTokenBytes))
}

func requireExpired(t *testing.T, err error) {
	t.Helper()
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote.Code != protocol.ErrContentRefExpired {
		t.Fatalf("error = %T %v, want CONTENT_REF_EXPIRED", err, err)
	}
}

func readAll(t *testing.T, store *Store, owner Owner, descriptor protocol.ExternalizedField) []byte {
	t.Helper()
	var out []byte
	for offset := int64(0); ; {
		result, err := store.ReadForOwner(owner, protocol.SessionContentParams{
			ContentRef: descriptor.ContentRef,
			Offset:     offset,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := result.Validate(); err != nil {
			t.Fatalf("invalid session/content result: %v", err)
		}
		chunk, err := base64.StdEncoding.DecodeString(result.DataBase64)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, chunk...)
		if result.NextOffset == nil {
			break
		}
		offset = *result.NextOffset
	}
	if int64(len(out)) != descriptor.TotalBytes {
		t.Fatalf("read bytes = %d, descriptor = %d", len(out), descriptor.TotalBytes)
	}
	digest := sha256.Sum256(out)
	if got := hex.EncodeToString(digest[:]); got != descriptor.SHA256 {
		t.Fatalf("full content sha256 = %s, want %s", got, descriptor.SHA256)
	}
	return out
}

func TestExternalizeFieldThresholdAndCallerSelectionAreExact(t *testing.T) {
	store := newTestStore(t, Config{})
	exact := strings.Repeat("x", protocol.ExternalizeFieldBytes)

	inline, err := ExternalizeSessionEvent(store, testEvent(1, exact), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inline.Externalized) != 0 {
		t.Fatalf("exactly 64 KiB yielded %d descriptors, want inline", len(inline.Externalized))
	}
	if inline.Externalized == nil {
		t.Fatal("empty required externalized array remained JSON null")
	}

	selected, err := ExternalizeSessionEvent(store, testEvent(2, exact), ExternalizeOptions{
		AdditionalJSONPointers: []string{"/event/text"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected.Externalized) != 1 || selected.Externalized[0].JSONPointer != "/event/text" {
		t.Fatalf("caller-selected descriptors = %#v", selected.Externalized)
	}

	over, err := ExternalizeSessionEvent(store, testEvent(3, exact+"y"), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(over.Externalized) != 1 || over.Externalized[0].TotalBytes != int64(protocol.ExternalizeFieldBytes+1) {
		t.Fatalf("over-threshold descriptors = %#v", over.Externalized)
	}
}

func TestExternalizeRejectsInvalidScopeAndPointerRequests(t *testing.T) {
	if _, err := SnapshotOwner(""); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("empty snapshot owner error = %v", err)
	}
	if _, err := EventOwner("", 1); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("empty runtime owner error = %v", err)
	}
	if _, err := EventOwner("runtime-test", 0); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("zero-seq event owner error = %v", err)
	}
	if _, err := New("", Config{}); !errors.Is(err, ErrInvalidOwner) {
		t.Fatalf("empty host epoch error = %v", err)
	}
	if _, err := New(testHostEpoch, Config{MaxBytes: -1}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("negative capacity error = %v", err)
	}

	store := newTestStore(t, Config{})
	if _, err := ExternalizeSessionEvent(nil, testEvent(1, "text"), ExternalizeOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil store error = %v", err)
	}
	wrongEpoch := testEvent(1, "text")
	wrongEpoch.HostEpoch = "another-host-epoch"
	if _, err := ExternalizeSessionEvent(store, wrongEpoch, ExternalizeOptions{}); !errors.Is(err, ErrEpochMismatch) {
		t.Fatalf("wrong host epoch error = %v", err)
	}
	preExternalized := testEvent(1, "text")
	preExternalized.Externalized = []protocol.ExternalizedField{{JSONPointer: "/event/text"}}
	if _, err := ExternalizeSessionEvent(store, preExternalized, ExternalizeOptions{}); !errors.Is(err, ErrAlreadyExternalized) {
		t.Fatalf("pre-externalized owner error = %v", err)
	}
	if _, err := ExternalizeSessionEvent(store, testEvent(2, "text"), ExternalizeOptions{
		AdditionalJSONPointers: []string{"/event/text", "/event/text"},
	}); !errors.Is(err, ErrDuplicatePointer) {
		t.Fatalf("duplicate pointer error = %v", err)
	}
	if _, err := ExternalizeSessionEvent(store, testEvent(3, "text"), ExternalizeOptions{
		AdditionalJSONPointers: []string{"/event/code"},
	}); !errors.Is(err, ErrUnknownPointer) {
		t.Fatalf("non-externalizable pointer error = %v", err)
	}

	limited := newTestStore(t, Config{MaxBytes: 1, MaxEntries: 1})
	if _, err := ExternalizeSessionEvent(limited, testEvent(4, strings.Repeat("x", 70<<10)), ExternalizeOptions{}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("bounded store capacity error = %v", err)
	}
	if stats := limited.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatalf("rejected batch changed store: %#v", stats)
	}
}

func TestSessionContentUsesRawUnicodeByteOffsetsAndExactChunks(t *testing.T) {
	store := newTestStore(t, Config{})
	text := "世" + strings.Repeat("z", protocol.ContentRefChunkBytes+8)
	externalized, err := ExternalizeSessionEvent(store, testEvent(1, text), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := externalized.Externalized[0]
	owner, _ := EventOwner(externalized.RuntimeEpoch, externalized.Seq)

	first, err := store.ReadForOwner(owner, protocol.SessionContentParams{ContentRef: descriptor.ContentRef, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := base64.StdEncoding.DecodeString(first.DataBase64)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunk) != protocol.ContentRefChunkBytes {
		t.Fatalf("chunk bytes = %d, want %d", len(chunk), protocol.ContentRefChunkBytes)
	}
	if !bytes.Equal(chunk, []byte(text)[1:1+protocol.ContentRefChunkBytes]) {
		t.Fatal("offset was interpreted as a rune or Base64 offset")
	}
	if first.NextOffset == nil || *first.NextOffset != 1+protocol.ContentRefChunkBytes {
		t.Fatalf("nextOffset = %v", first.NextOffset)
	}
	digest := sha256.Sum256([]byte(text))
	if first.SHA256 != hex.EncodeToString(digest[:]) || first.TotalBytes != int64(len([]byte(text))) {
		t.Fatalf("content metadata = sha %s bytes %d", first.SHA256, first.TotalBytes)
	}

	final, err := store.Read(protocol.SessionContentParams{ContentRef: descriptor.ContentRef, Offset: *first.NextOffset})
	if err != nil {
		t.Fatal(err)
	}
	if final.NextOffset != nil {
		t.Fatalf("final nextOffset = %v", final.NextOffset)
	}
}

func TestTypedOwnersBindSnapshotHistoryAndEventReferences(t *testing.T) {
	store := newTestStore(t, Config{})
	content := strings.Repeat("history", 10_000)
	page := protocol.HistoryPage{
		SnapshotID: "snapshot-a",
		Messages:   []protocol.HistoryMessage{{Role: "assistant", Content: &content}},
		StartTurn:  0, EndTurn: 1, TotalTurns: 1, ActualTurns: 1,
	}
	externalizedPage, err := ExternalizeHistoryPage(store, page, ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(externalizedPage.Externalized) != 1 {
		t.Fatalf("history descriptors = %d", len(externalizedPage.Externalized))
	}
	correctSnapshot, _ := SnapshotOwner("snapshot-a")
	wrongSnapshot, _ := SnapshotOwner("snapshot-b")
	params := protocol.SessionContentParams{ContentRef: externalizedPage.Externalized[0].ContentRef}
	if _, err := store.ReadForOwner(correctSnapshot, params); err != nil {
		t.Fatalf("correct snapshot binding rejected: %v", err)
	}
	_, err = store.ReadForOwner(wrongSnapshot, params)
	requireExpired(t, err)
	crossKindOwner, _ := EventOwner("snapshot-a", 1)
	_, err = store.ReadForOwner(crossKindOwner, params)
	requireExpired(t, err)

	goal := strings.Repeat("goal", 20_000)
	snapshot := protocol.SessionSnapshot{
		SnapshotID: "snapshot-a", HostEpoch: testHostEpoch,
		Target:       protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-test"},
		RuntimeEpoch: "runtime-test",
		Meta:         protocol.SessionMetaSnapshot{Goal: &goal},
		Runtime:      protocol.SessionRuntimeState{LiveEvents: []eventwire.Event{}},
		History: protocol.HistoryPage{
			SnapshotID: "snapshot-a", Messages: []protocol.HistoryMessage{},
		},
		Todos: []protocol.TodoItem{}, Jobs: []protocol.JobView{}, Checkpoints: []protocol.CheckpointView{},
	}
	externalizedSnapshot, err := ExternalizeSessionSnapshot(store, snapshot, ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if externalizedSnapshot.History.Externalized == nil {
		t.Fatal("snapshot nested history externalized array remained JSON null")
	}
	if len(externalizedSnapshot.Externalized) != 1 || externalizedSnapshot.Externalized[0].JSONPointer != "/meta/goal" {
		t.Fatalf("snapshot descriptors = %#v", externalizedSnapshot.Externalized)
	}
	if _, err := store.ReadForOwner(correctSnapshot, protocol.SessionContentParams{ContentRef: externalizedSnapshot.Externalized[0].ContentRef}); err != nil {
		t.Fatalf("snapshot/history shared snapshotId owner rejected: %v", err)
	}

	event, err := ExternalizeSessionEvent(store, testEvent(7, strings.Repeat("event", 20_000)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	eventOwner, _ := EventOwner(event.RuntimeEpoch, 7)
	wrongEventOwner, _ := EventOwner(event.RuntimeEpoch, 8)
	eventParams := protocol.SessionContentParams{ContentRef: event.Externalized[0].ContentRef}
	if _, err := store.ReadForOwner(eventOwner, eventParams); err != nil {
		t.Fatal(err)
	}
	_, err = store.ReadForOwner(wrongEventOwner, eventParams)
	requireExpired(t, err)
}

func TestLeaseBindingSurvivesResumeAndRejectsAnotherLease(t *testing.T) {
	store := newTestStore(t, Config{})
	event, err := ExternalizeSessionEvent(store, testEvent(1, strings.Repeat("lease", 20_000)), ExternalizeOptions{
		LeaseID: "lease-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionContentParams{ContentRef: event.Externalized[0].ContentRef}
	if _, err := store.ReadForLease("lease-a", params); err != nil {
		t.Fatalf("issuing/resumed lease could not read its content: %v", err)
	}
	_, binding, err := store.ReadBoundForLease("lease-a", params)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Kind != ReferenceEvent || binding.HostEpoch != testHostEpoch || binding.LeaseID != "lease-a" ||
		binding.Target != event.Target || binding.RuntimeEpoch != event.RuntimeEpoch || binding.Seq != event.Seq {
		t.Fatalf("event reference binding = %+v", binding)
	}
	_, err = store.ReadForLease("lease-b", params)
	requireExpired(t, err)
	_, err = store.ReadForLease("", params)
	requireExpired(t, err)

	if _, err := ExternalizeSessionEvent(store, testEvent(2, strings.Repeat("bad", 30_000)), ExternalizeOptions{
		LeaseID: "   ",
	}); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("whitespace lease error = %v", err)
	}
}

func TestHistoryBudgetAutomaticallyExternalizesSmallTaggedFields(t *testing.T) {
	store := newTestStore(t, Config{})
	const turns = 40
	messages := make([]protocol.HistoryMessage, turns)
	for i := range messages {
		body := fmt.Sprintf("%02d", i) + strings.Repeat("b", 60<<10-2)
		messages[i] = protocol.HistoryMessage{Role: "assistant", Content: &body}
	}
	page := protocol.HistoryPage{
		SnapshotID: "snapshot-budget", Messages: messages,
		StartTurn: 0, EndTurn: turns, TotalTurns: turns, ActualTurns: turns,
	}
	externalized, err := ExternalizeHistoryPage(store, page, ExternalizeOptions{
		AdditionalJSONPointers: []string{"/messages/39/content"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(externalized.Externalized) == 0 {
		t.Fatal("over-budget page retained all sub-64KiB fields inline")
	}
	foundSelected := false
	for _, descriptor := range externalized.Externalized {
		if descriptor.TotalBytes > protocol.ExternalizeFieldBytes {
			t.Fatalf("budget test unexpectedly used threshold field: %d", descriptor.TotalBytes)
		}
		if descriptor.JSONPointer == "/messages/39/content" {
			foundSelected = true
		}
	}
	if !foundSelected {
		t.Fatal("caller-selected pointer was not externalized")
	}
	wire, err := json.Marshal(externalized)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) > protocol.SnapshotHistoryBytes {
		t.Fatalf("history wire bytes = %d, limit = %d", len(wire), protocol.SnapshotHistoryBytes)
	}
}

func TestEventUsesFrameBudgetNotSnapshotHistoryBudget(t *testing.T) {
	store := newTestStore(t, Config{})
	event := testEvent(1, "")
	event.Externalized = []protocol.ExternalizedField{}
	event.Event.Code = "c"
	probe, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	codeBytes := sessionEventPayloadBytes - (len(probe) - 1)
	if codeBytes <= protocol.SnapshotHistoryBytes {
		t.Fatalf("computed event field budget = %d", codeBytes)
	}
	event.Event.Code = strings.Repeat("c", codeBytes)

	result, err := ExternalizeSessionEvent(store, event, ExternalizeOptions{})
	if err != nil {
		t.Fatalf("event below the Remote frame budget was rejected: %v", err)
	}
	if len(result.Externalized) != 0 {
		t.Fatalf("bounded event field was unexpectedly externalized: %#v", result.Externalized)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) != sessionEventPayloadBytes {
		t.Fatalf("event wire bytes = %d, want exact payload budget %d", len(wire), sessionEventPayloadBytes)
	}
	frame := append([]byte(`{"jsonrpc":"2.0","method":"session/event","params":`), wire...)
	frame = append(frame, '}', '\n')
	if len(frame)-len(wire) != sessionEventFrameOverhead || len(frame) > protocol.FrameBytes {
		t.Fatalf("notification frame bytes = %d, payload = %d, overhead = %d", len(frame), len(wire), sessionEventFrameOverhead)
	}

	event.Event.Code += "c"
	overWire, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if len(overWire) != sessionEventPayloadBytes+1 {
		t.Fatalf("over-budget event wire bytes = %d, want %d", len(overWire), sessionEventPayloadBytes+1)
	}
	if _, err := ExternalizeSessionEvent(store, event, ExternalizeOptions{}); !errors.Is(err, ErrOwnerBudget) {
		t.Fatalf("over-frame event error = %v, want ErrOwnerBudget", err)
	}
}

func TestSnapshotWireBudgetBoundaryIsExact(t *testing.T) {
	store := newTestStore(t, Config{})
	emptyGoal := ""
	snapshot := protocol.SessionSnapshot{
		SnapshotID: "snapshot-boundary", HostEpoch: testHostEpoch,
		Target:       protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-test"},
		RuntimeEpoch: "runtime-test",
		Meta:         protocol.SessionMetaSnapshot{Goal: &emptyGoal},
		Runtime:      protocol.SessionRuntimeState{LiveEvents: []eventwire.Event{}},
		History: protocol.HistoryPage{
			SnapshotID: "snapshot-boundary", Messages: []protocol.HistoryMessage{},
			Externalized: []protocol.ExternalizedField{},
		},
		Todos: []protocol.TodoItem{}, Jobs: []protocol.JobView{}, Checkpoints: []protocol.CheckpointView{{
			CheckpointID: "checkpoint-test", Files: []string{"f"},
		}},
		Externalized: []protocol.ExternalizedField{},
	}
	probe, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	fileBytes := protocol.SnapshotHistoryBytes - (len(probe) - 1)
	if fileBytes <= 0 {
		t.Fatalf("computed snapshot field budget = %d", fileBytes)
	}
	snapshot.Checkpoints[0].Files[0] = strings.Repeat("f", fileBytes)

	result, err := ExternalizeSessionSnapshot(store, snapshot, ExternalizeOptions{})
	if err != nil {
		t.Fatalf("exact-budget snapshot was rejected: %v", err)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) != protocol.SnapshotHistoryBytes {
		t.Fatalf("snapshot wire bytes = %d, want %d", len(wire), protocol.SnapshotHistoryBytes)
	}

	snapshot.Checkpoints[0].Files[0] += "f"
	if _, err := ExternalizeSessionSnapshot(store, snapshot, ExternalizeOptions{}); !errors.Is(err, ErrOwnerBudget) {
		t.Fatalf("over-budget snapshot error = %v, want ErrOwnerBudget", err)
	}
}

func TestOwnerBudgetRejectsOversizedNonExternalizableState(t *testing.T) {
	store := newTestStore(t, Config{})
	snapshot := protocol.SessionSnapshot{
		SnapshotID: "snapshot-too-large", HostEpoch: testHostEpoch,
		Target:       protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-test"},
		RuntimeEpoch: "runtime-test",
		Runtime:      protocol.SessionRuntimeState{LiveEvents: []eventwire.Event{}},
		History: protocol.HistoryPage{
			SnapshotID: "snapshot-too-large", Messages: []protocol.HistoryMessage{},
		},
		Todos: []protocol.TodoItem{}, Jobs: []protocol.JobView{},
		Checkpoints: []protocol.CheckpointView{{
			CheckpointID: "checkpoint-test",
			Files:        []string{strings.Repeat("f", protocol.SnapshotHistoryBytes+1024)},
		}},
	}
	if _, err := ExternalizeSessionSnapshot(store, snapshot, ExternalizeOptions{}); !errors.Is(err, ErrOwnerBudget) {
		t.Fatalf("oversized non-externalizable owner error = %v", err)
	}
	if stats := store.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatalf("failed owner retained content: %#v", stats)
	}
}

func TestObjectLimitHeadTailTruncationAndExactBoundary(t *testing.T) {
	store := newTestStore(t, Config{})
	exact := strings.Repeat("x", protocol.ContentRefObjectBytes)
	exactEvent, err := ExternalizeSessionEvent(store, testEvent(1, exact), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	exactDescriptor := exactEvent.Externalized[0]
	if exactDescriptor.Truncated || exactDescriptor.OriginalBytes != nil || exactDescriptor.TotalBytes != protocol.ContentRefObjectBytes {
		t.Fatalf("exact 8 MiB descriptor = %#v", exactDescriptor)
	}

	original := "HEAD" + strings.Repeat("界", protocol.ContentRefObjectBytes/3+100) + "TAIL"
	truncatedEvent, err := ExternalizeSessionEvent(store, testEvent(2, original), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := truncatedEvent.Externalized[0]
	if !descriptor.Truncated || descriptor.OriginalBytes == nil || *descriptor.OriginalBytes != int64(len([]byte(original))) {
		t.Fatalf("truncation metadata = %#v", descriptor)
	}
	if descriptor.TruncationReason != ContentObjectLimitReason || descriptor.TotalBytes > protocol.ContentRefObjectBytes {
		t.Fatalf("truncation reason/bytes = %q/%d", descriptor.TruncationReason, descriptor.TotalBytes)
	}
	owner, _ := EventOwner(truncatedEvent.RuntimeEpoch, truncatedEvent.Seq)
	stored := readAll(t, store, owner, descriptor)
	if !utf8.Valid(stored) || !bytes.HasPrefix(stored, []byte("HEAD")) || !bytes.HasSuffix(stored, []byte("TAIL")) {
		t.Fatal("head-tail truncation did not preserve valid UTF-8 prefix and suffix")
	}
}

func TestWireNullThenTypedRehydration(t *testing.T) {
	store := newTestStore(t, Config{})
	text := strings.Repeat("内容", 40_000)
	event, err := ExternalizeSessionEvent(store, testEvent(1, text), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	eventTree := tree["event"].(map[string]any)
	if value, present := eventTree["text"]; !present || value != nil {
		t.Fatalf("wire event.text = %#v, present=%v; want explicit null", value, present)
	}

	owner, _ := EventOwner(event.RuntimeEpoch, event.Seq)
	replacements := make([]protocol.RehydratedExternalizedField, 0, len(event.Externalized))
	for _, descriptor := range event.Externalized {
		replacements = append(replacements, protocol.RehydratedExternalizedField{
			JSONPointer: descriptor.JSONPointer,
			Value:       string(readAll(t, store, owner, descriptor)),
		})
	}
	decoded, err := protocol.DecodeRehydratedJSON[protocol.SessionEvent](raw, replacements)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Event.Text != text || decoded.RuntimeEpoch != event.RuntimeEpoch || decoded.Seq != event.Seq {
		t.Fatal("typed rehydration changed event semantics or owner identity")
	}
}

func TestContentRefIdleAndAbsoluteExpiry(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	now := start
	store := newTestStore(t, Config{Now: func() time.Time { return now }})
	event, err := ExternalizeSessionEvent(store, testEvent(1, strings.Repeat("x", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionContentParams{ContentRef: event.Externalized[0].ContentRef}

	now = start.Add(14 * time.Minute)
	if _, err := store.Read(params); err != nil {
		t.Fatal(err)
	}
	now = start.Add(29 * time.Minute)
	_, err = store.Read(params)
	requireExpired(t, err) // exact 15-minute idle boundary
	if stats := store.Stats(); stats.Entries != 0 || stats.Owners != 0 {
		t.Fatalf("idle expiry retained owner metadata: %#v", stats)
	}

	now = start
	absolute := newTestStore(t, Config{Now: func() time.Time { return now }})
	event, err = ExternalizeSessionEvent(absolute, testEvent(2, strings.Repeat("y", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	params = protocol.SessionContentParams{ContentRef: event.Externalized[0].ContentRef}
	for minute := 10; minute < 60; minute += 10 {
		now = start.Add(time.Duration(minute) * time.Minute)
		if _, err := absolute.Read(params); err != nil {
			t.Fatalf("minute %d: %v", minute, err)
		}
	}
	now = start.Add(time.Hour)
	_, err = absolute.Read(params)
	requireExpired(t, err) // absolute age wins despite reads
	if stats := absolute.Stats(); stats.Entries != 0 || stats.Owners != 0 {
		t.Fatalf("absolute expiry retained owner metadata: %#v", stats)
	}
}

func TestReleasedIDsNeverReuseAfterGeneratorCollision(t *testing.T) {
	ids := []protocol.ContentRef{testRef('A'), testRef('A'), testRef('B')}
	index := 0
	store := newTestStore(t, Config{newID: func() (protocol.ContentRef, error) {
		if index >= len(ids) {
			return "", errors.New("unexpected ID request")
		}
		ref := ids[index]
		index++
		return ref, nil
	}})
	first, err := ExternalizeSessionEvent(store, testEvent(1, strings.Repeat("a", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Externalized[0].ContentRef != testRef('A') || !store.Release(testRef('A')) {
		t.Fatal("first reference was not issued and released as expected")
	}
	second, err := ExternalizeSessionEvent(store, testEvent(2, strings.Repeat("b", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Externalized[0].ContentRef != testRef('B') {
		t.Fatalf("released ID was reused: %q", second.Externalized[0].ContentRef)
	}
	if stats := store.Stats(); stats.Issued != 2 {
		t.Fatalf("issued IDs = %d, want 2 lifetime-unique IDs", stats.Issued)
	}
}

func TestProductionReferencesAreEpochStoreSecretAndLifetimeUnique(t *testing.T) {
	firstStore := newTestStore(t, Config{})
	secondStore := newTestStore(t, Config{})
	first, err := ExternalizeSessionEvent(firstStore, testEvent(1, strings.Repeat("a", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	otherStoreFirst, err := ExternalizeSessionEvent(secondStore, testEvent(1, strings.Repeat("a", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	firstRef := first.Externalized[0].ContentRef
	if !validGeneratedRef(firstRef) || !validGeneratedRef(otherStoreFirst.Externalized[0].ContentRef) {
		t.Fatal("production issuer returned a malformed opaque reference")
	}
	if firstRef == otherStoreFirst.Externalized[0].ContentRef {
		t.Fatal("independent epoch stores deterministically issued the same reference")
	}
	if !firstStore.Release(firstRef) {
		t.Fatal("failed to release first production reference")
	}
	second, err := ExternalizeSessionEvent(firstStore, testEvent(2, strings.Repeat("b", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Externalized[0].ContentRef == firstRef {
		t.Fatal("production issuer reused a released reference")
	}
}

func TestBoundedEvictionReleaseAndEpochCloseNeverReturnWrongContent(t *testing.T) {
	store := newTestStore(t, Config{MaxBytes: 90 << 10, MaxEntries: 2})
	firstText := strings.Repeat("a", 70<<10)
	first, err := ExternalizeSessionEvent(store, testEvent(1, firstText), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secondText := strings.Repeat("b", 70<<10)
	second, err := ExternalizeSessionEvent(store, testEvent(2, secondText), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Read(protocol.SessionContentParams{ContentRef: first.Externalized[0].ContentRef})
	requireExpired(t, err)
	secondResult, err := store.Read(protocol.SessionContentParams{ContentRef: second.Externalized[0].ContentRef})
	if err != nil {
		t.Fatal(err)
	}
	secondChunk, _ := base64.StdEncoding.DecodeString(secondResult.DataBase64)
	if !bytes.Equal(secondChunk, []byte(secondText)) {
		t.Fatal("evicted ref returned another entry's bytes")
	}
	if stats := store.Stats(); stats.Bytes > 90<<10 || stats.Entries > 2 {
		t.Fatalf("unbounded store stats: %#v", stats)
	}

	owner, _ := EventOwner(second.RuntimeEpoch, second.Seq)
	if released := store.ReleaseOwner(owner); released != 1 {
		t.Fatalf("ReleaseOwner released %d refs, want 1", released)
	}
	_, err = store.Read(protocol.SessionContentParams{ContentRef: second.Externalized[0].ContentRef})
	requireExpired(t, err)

	third, err := ExternalizeSessionEvent(store, testEvent(3, strings.Repeat("c", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	_, err = store.Read(protocol.SessionContentParams{ContentRef: third.Externalized[0].ContentRef})
	requireExpired(t, err)
	if _, err := ExternalizeSessionEvent(store, testEvent(4, strings.Repeat("d", 70<<10)), ExternalizeOptions{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("externalize after Close error = %v", err)
	}
}

func TestInvalidUTF8AndOffsetsAreRejected(t *testing.T) {
	store := newTestStore(t, Config{})
	inlineInvalid := string([]byte{0xff})
	if _, err := ExternalizeSessionEvent(store, testEvent(1, inlineInvalid), ExternalizeOptions{}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("inline invalid UTF-8 error = %v", err)
	}
	invalid := string(bytes.Repeat([]byte{0xff}, 70<<10))
	if _, err := ExternalizeSessionEvent(store, testEvent(2, invalid), ExternalizeOptions{}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
	event, err := ExternalizeSessionEvent(store, testEvent(3, strings.Repeat("x", 70<<10)), ExternalizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ref := event.Externalized[0].ContentRef
	if _, err := store.Read(protocol.SessionContentParams{ContentRef: ref, Offset: -1}); !errors.Is(err, ErrInvalidOffset) {
		t.Fatalf("negative offset error = %v", err)
	}
	if _, err := store.Read(protocol.SessionContentParams{ContentRef: ref, Offset: int64(70<<10) + 1}); !errors.Is(err, ErrInvalidOffset) {
		t.Fatalf("past-end offset error = %v", err)
	}
	_, err = store.Read(protocol.SessionContentParams{ContentRef: testRef('Z')})
	requireExpired(t, err)
}

func TestConcurrentExternalizeReadReleaseStress(t *testing.T) {
	store := newTestStore(t, Config{MaxBytes: 16 << 20, MaxEntries: 256})
	const workers = 8
	const iterations = 40
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				seq := uint64(worker*iterations + iteration + 1)
				prefix := fmt.Sprintf("worker=%d iteration=%d ", worker, iteration)
				body := prefix + strings.Repeat(string(rune('a'+worker)), 70<<10-len(prefix))
				event, err := ExternalizeSessionEvent(store, testEvent(seq, body), ExternalizeOptions{})
				if err != nil {
					errCh <- err
					return
				}
				owner, _ := EventOwner(event.RuntimeEpoch, event.Seq)
				result, err := store.ReadForOwner(owner, protocol.SessionContentParams{ContentRef: event.Externalized[0].ContentRef})
				if err != nil {
					errCh <- err
					return
				}
				chunk, err := base64.StdEncoding.DecodeString(result.DataBase64)
				if err != nil || !bytes.Equal(chunk, []byte(body)) {
					errCh <- fmt.Errorf("content crossed references for worker %d iteration %d", worker, iteration)
					return
				}
				if store.ReleaseOwner(owner) != 1 {
					errCh <- errors.New("owner release did not remove exactly one reference")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if stats := store.Stats(); stats.Entries != 0 || stats.Bytes != 0 || stats.Issued != workers*iterations {
		t.Fatalf("stress stats = %#v", stats)
	}
}
