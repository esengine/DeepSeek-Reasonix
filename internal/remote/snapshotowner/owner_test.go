package snapshotowner

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/remote/contentref"
	"reasonix/internal/remote/history"
	"reasonix/internal/remote/protocol"
)

const testHostEpoch protocol.HostEpoch = "host-snapshotowner"

func ownerBinding(name string) history.Binding {
	return history.Binding{
		SnapshotID: protocol.SnapshotID("snapshot-" + name),
		HostEpoch:  testHostEpoch,
		Target: protocol.RuntimeTarget{
			WorkspaceID: protocol.WorkspaceID("workspace-" + name),
			SessionID:   protocol.SessionID("session-" + name),
		},
		RuntimeEpoch: protocol.RuntimeEpoch("runtime-" + name),
	}
}

func ownerBase(binding history.Binding) protocol.SessionSnapshot {
	return protocol.SessionSnapshot{
		SnapshotID:   binding.SnapshotID,
		HostEpoch:    binding.HostEpoch,
		Target:       binding.Target,
		RuntimeEpoch: binding.RuntimeEpoch,
		Meta: protocol.SessionMetaSnapshot{
			TopicID: "topic-test",
			Title:   "Snapshot owner test",
			ResolvedProfile: protocol.ResolvedProfile{
				Model: "model", Effort: "medium", CollaborationMode: protocol.CollaborationNormal,
				TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
			},
			Capabilities: protocol.FrozenCapabilities(true, true),
		},
		Runtime: protocol.SessionRuntimeState{LiveEvents: []eventwire.Event{}},
		History: protocol.HistoryPage{
			SnapshotID: binding.SnapshotID, Messages: []protocol.HistoryMessage{}, Externalized: []protocol.ExternalizedField{},
		},
		Todos:        []protocol.TodoItem{},
		Context:      protocol.ContextView{Sources: []protocol.UsageSourceView{}, ReadFiles: []protocol.ReadFileRecord{}},
		Jobs:         []protocol.JobView{},
		Checkpoints:  []protocol.CheckpointView{},
		Externalized: []protocol.ExternalizedField{},
	}
}

func ownerCapture(binding history.Binding, turns int) history.Capture {
	messages := []provider.Message{{Role: provider.RoleSystem, Content: "system prefix"}}
	for turn := 0; turn < turns; turn++ {
		messages = append(messages,
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("user-%03d", turn)},
			provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("assistant-%03d", turn)},
		)
	}
	return history.Capture{Binding: binding, Messages: messages, Supplemental: []history.SupplementalMessage{}}
}

func newOwnerBuilder(
	t *testing.T,
	historyOptions history.Options,
	contentConfig contentref.Config,
) (*Builder, *history.Store, *contentref.Store) {
	t.Helper()
	if historyOptions.SweepInterval == 0 {
		historyOptions.SweepInterval = -1
	}
	histories, err := history.New(historyOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(histories.Close)
	contents, err := contentref.New(testHostEpoch, contentConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(contents.Close)
	builder, err := New(histories, contents)
	if err != nil {
		t.Fatal(err)
	}
	return builder, histories, contents
}

func historyContent(message protocol.HistoryMessage) string {
	if message.Content == nil {
		return ""
	}
	return *message.Content
}

func requireRemoteCode(t *testing.T, err error, code protocol.ReasonixErrorCode) {
	t.Helper()
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func readAllForLease(t *testing.T, store *contentref.Store, leaseID protocol.LeaseID, ref protocol.ContentRef) []byte {
	t.Helper()
	var out []byte
	offset := int64(0)
	for {
		result, err := store.ReadForLease(leaseID, protocol.SessionContentParams{ContentRef: ref, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		chunk, err := base64.StdEncoding.DecodeString(result.DataBase64)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, chunk...)
		if result.NextOffset == nil {
			return out
		}
		offset = *result.NextOffset
	}
}

func TestBuildSubscribeSnapshotUsesCompleteOwnerBudgetForSmallFields(t *testing.T) {
	builder, histories, contents := newOwnerBuilder(t, history.Options{}, contentref.Config{})
	binding := ownerBinding("small-fields")
	base := ownerBase(binding)
	questions := make([]protocol.AskQuestion, 40)
	for index := range questions {
		text := fmt.Sprintf("%02d", index) + strings.Repeat("q", (60<<10)-2)
		questions[index] = protocol.AskQuestion{
			QuestionID: protocol.QuestionID(fmt.Sprintf("question-%02d", index)),
			Prompt:     &text,
			Options:    []protocol.AskOption{},
		}
	}
	base.PendingPrompt = &protocol.PendingPrompt{
		Kind: protocol.PromptAsk,
		Ask:  &protocol.AskPrompt{PromptID: "prompt-small-fields", Questions: questions},
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= protocol.SnapshotHistoryBytes {
		t.Fatalf("test setup raw snapshot = %d bytes, want over %d", len(raw), protocol.SnapshotHistoryBytes)
	}

	result, err := builder.BuildSubscribeSnapshot(base, ownerCapture(binding, 1), 1, "lease-small-fields")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) > protocol.SnapshotHistoryBytes || len(result.Externalized) == 0 {
		t.Fatalf("final snapshot bytes=%d descriptors=%d", len(wire), len(result.Externalized))
	}
	if len(result.History.Externalized) != 0 {
		t.Fatalf("nested history was externalized separately: %+v", result.History.Externalized)
	}
	for _, descriptor := range result.Externalized {
		if descriptor.TotalBytes > protocol.ExternalizeFieldBytes {
			t.Fatalf("test unexpectedly relied on >64KiB threshold field: %+v", descriptor)
		}
	}
	first := result.Externalized[0]
	if body := readAllForLease(t, contents, "lease-small-fields", first.ContentRef); len(body) != int(first.TotalBytes) {
		t.Fatalf("externalized body bytes = %d, want %d", len(body), first.TotalBytes)
	}
	if !histories.Valid(binding) {
		t.Fatal("successful subscribe did not retain history")
	}
}

func TestBuildSubscribeSnapshotFallsBackOnlyAtWholeTurnBoundaries(t *testing.T) {
	builder, histories, _ := newOwnerBuilder(t, history.Options{}, contentref.Config{})
	binding := ownerBinding("whole-turn")
	capture := ownerCapture(binding, 20)
	empty := ""
	largeCode := strings.Repeat("C", 150_000)
	for turn := 0; turn < 20; turn++ {
		capture.Supplemental = append(capture.Supplemental, history.SupplementalMessage{
			AfterMessageIndex: 2 + 2*turn,
			Message: protocol.HistoryMessage{
				Role: "notice", Content: &empty, Code: largeCode,
			},
		})
	}
	result, err := builder.BuildSubscribeSnapshot(ownerBase(binding), capture, 20, "lease-whole-turn")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	page := result.History
	if len(wire) > protocol.SnapshotHistoryBytes || page.ActualTurns < 1 || page.ActualTurns >= 20 ||
		page.EndTurn != 20 || page.StartTurn != 20-page.ActualTurns || !page.HasOlder {
		t.Fatalf("whole-turn fallback page=%+v wire=%d", page, len(wire))
	}
	if len(page.Messages) != page.ActualTurns*3 {
		t.Fatalf("fallback split a turn: messages=%d turns=%d", len(page.Messages), page.ActualTurns)
	}
	if page.Messages[0].Role != "user" || historyContent(page.Messages[0]) != fmt.Sprintf("user-%03d", page.StartTurn) {
		t.Fatalf("first retained complete turn = %+v", page.Messages[0])
	}
	if len(page.Externalized) != 0 {
		t.Fatalf("nested history descriptors = %+v", page.Externalized)
	}
	if stats := histories.Stats(); stats.Snapshots != 1 || stats.Cursors != 1 {
		t.Fatalf("failed owner-budget probes leaked history state: %+v", stats)
	}
}

func TestBuildSubscribeSnapshotTruncatesSingleOversizedObject(t *testing.T) {
	builder, _, contents := newOwnerBuilder(t, history.Options{}, contentref.Config{})
	binding := ownerBinding("object-limit")
	original := "HEAD" + strings.Repeat("界", protocol.ContentRefObjectBytes/3+100) + "TAIL"
	capture := history.Capture{Binding: binding, Messages: []provider.Message{
		{Role: provider.RoleUser, Content: "one turn"},
		{Role: provider.RoleAssistant, Content: original},
	}}
	result, err := builder.BuildSubscribeSnapshot(ownerBase(binding), capture, 1, "lease-object-limit")
	if err != nil {
		t.Fatal(err)
	}
	var descriptor *protocol.ExternalizedField
	for index := range result.Externalized {
		if strings.HasSuffix(result.Externalized[index].JSONPointer, "/content") && result.Externalized[index].Truncated {
			descriptor = &result.Externalized[index]
			break
		}
	}
	if descriptor == nil || descriptor.OriginalBytes == nil || *descriptor.OriginalBytes != int64(len([]byte(original))) ||
		descriptor.TotalBytes > protocol.ContentRefObjectBytes || descriptor.TruncationReason != contentref.ContentObjectLimitReason {
		t.Fatalf("oversized descriptor = %+v", descriptor)
	}
	stored := readAllForLease(t, contents, "lease-object-limit", descriptor.ContentRef)
	if len(stored) != int(descriptor.TotalBytes) || !utf8.Valid(stored) || !strings.HasPrefix(string(stored), "HEAD") || !strings.HasSuffix(string(stored), "TAIL") {
		t.Fatalf("truncated object bytes=%d valid=%v", len(stored), utf8.Valid(stored))
	}
}

func TestBuildOlderHistorySharesOwnerLeaseAndReturnsDeepCopies(t *testing.T) {
	builder, histories, contents := newOwnerBuilder(t, history.Options{}, contentref.Config{})
	binding := ownerBinding("older")
	capture := ownerCapture(binding, 5)
	for index := range capture.Messages {
		if capture.Messages[index].Role == provider.RoleAssistant {
			capture.Messages[index].Content += strings.Repeat("A", 70<<10)
		}
	}
	todo := "keep todo"
	base := ownerBase(binding)
	base.Todos = []protocol.TodoItem{{Content: &todo, Status: protocol.TodoPending}}
	latest, err := builder.BuildSubscribeSnapshot(base, capture, 2, "lease-older")
	if err != nil {
		t.Fatal(err)
	}
	if latest.History.StartTurn != 3 || latest.History.NextCursor == "" || len(latest.Externalized) == 0 {
		t.Fatalf("latest snapshot history = %+v descriptors=%d", latest.History, len(latest.Externalized))
	}

	// Both the base and caller-owned capture may change immediately after Build.
	// The returned owner and retained history remain immutable.
	todo = "mutated original todo"
	capture.Messages[1].Content = "mutated original user"
	if latest.Todos[0].Content == nil || *latest.Todos[0].Content != "keep todo" {
		t.Fatalf("returned snapshot aliases base: %+v", latest.Todos)
	}
	*latest.Todos[0].Content = "mutated returned todo"
	if todo != "mutated original todo" {
		t.Fatalf("returned snapshot mutated base pointer: %q", todo)
	}

	older, err := builder.BuildOlderHistory(binding, latest.History.NextCursor, 2, "lease-older")
	if err != nil {
		t.Fatal(err)
	}
	if older.StartTurn != 1 || older.EndTurn != 3 || older.ActualTurns != 2 || len(older.Externalized) == 0 {
		t.Fatalf("older page = %+v", older)
	}
	descriptor := older.Externalized[0]
	if body := readAllForLease(t, contents, "lease-older", descriptor.ContentRef); len(body) != int(descriptor.TotalBytes) {
		t.Fatalf("older content bytes=%d want=%d", len(body), descriptor.TotalBytes)
	}
	_, reference, err := contents.ReadBoundForLease("lease-older", protocol.SessionContentParams{ContentRef: descriptor.ContentRef})
	if err != nil {
		t.Fatal(err)
	}
	if reference.Kind != contentref.ReferenceSnapshot || reference.HostEpoch != binding.HostEpoch || reference.LeaseID != "lease-older" ||
		reference.Target != binding.Target || reference.RuntimeEpoch != binding.RuntimeEpoch || reference.SnapshotID != binding.SnapshotID {
		t.Fatalf("older reference binding = %+v", reference)
	}
	_, err = contents.ReadForLease("lease-other", protocol.SessionContentParams{ContentRef: descriptor.ContentRef})
	requireRemoteCode(t, err, protocol.ErrContentRefExpired)
	wrongEpoch := binding
	wrongEpoch.HostEpoch = "host-other"
	if released, refs := builder.Release(wrongEpoch); released || refs != 0 {
		t.Fatalf("cross-epoch Release = %v, %d", released, refs)
	}
	if _, err := contents.ReadForLease("lease-older", protocol.SessionContentParams{ContentRef: descriptor.ContentRef}); err != nil {
		t.Fatalf("cross-epoch Release invalidated the real owner: %v", err)
	}

	// Reusing the immutable cursor produces a fresh deep copy, not aliases to
	// either the retained page or a previous return value.
	for index := range older.Messages {
		if older.Messages[index].Role == "user" && older.Messages[index].Content != nil {
			*older.Messages[index].Content = "mutated returned history"
			break
		}
	}
	again, err := builder.BuildOlderHistory(binding, latest.History.NextCursor, 2, "lease-older")
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Messages) == 0 || historyContent(again.Messages[0]) != "user-001" {
		t.Fatalf("retained older history was mutated: %+v", again.Messages)
	}

	historyReleased, refsReleased := builder.Release(binding)
	if !historyReleased || refsReleased == 0 || histories.Valid(binding) {
		t.Fatalf("release = history:%v refs:%d valid:%v", historyReleased, refsReleased, histories.Valid(binding))
	}
	if stats := contents.Stats(); stats.Entries != 0 || stats.Owners != 0 || stats.Bytes != 0 {
		t.Fatalf("release leaked content owner: %+v", stats)
	}
}

func TestHistoryExpiryEvictionMismatchAndCloseAreSnapshotExpired(t *testing.T) {
	t.Run("ttl", func(t *testing.T) {
		now := time.Unix(1_700_000_000, 0)
		builder, _, contents := newOwnerBuilder(t, history.Options{
			SnapshotTTL: time.Minute, Now: func() time.Time { return now },
		}, contentref.Config{})
		binding := ownerBinding("ttl")
		capture := ownerCapture(binding, 2)
		capture.Messages[len(capture.Messages)-1].Content += strings.Repeat("t", 70<<10)
		latest, err := builder.BuildSubscribeSnapshot(ownerBase(binding), capture, 1, "lease-ttl")
		if err != nil {
			t.Fatal(err)
		}
		if len(latest.Externalized) == 0 {
			t.Fatal("TTL setup did not create snapshot-owned content")
		}
		ref := latest.Externalized[0].ContentRef
		now = now.Add(time.Minute)
		_, err = builder.BuildOlderHistory(binding, latest.History.NextCursor, 1, "lease-ttl")
		requireRemoteCode(t, err, protocol.ErrSnapshotExpired)
		historyReleased, refsReleased := builder.Release(binding)
		if historyReleased || refsReleased == 0 {
			t.Fatalf("expired Release = history:%v refs:%d", historyReleased, refsReleased)
		}
		_, err = contents.ReadForLease("lease-ttl", protocol.SessionContentParams{ContentRef: ref})
		requireRemoteCode(t, err, protocol.ErrContentRefExpired)
	})

	t.Run("capacity eviction and binding mismatch", func(t *testing.T) {
		builder, histories, contents := newOwnerBuilder(t, history.Options{MaxSnapshots: 1}, contentref.Config{})
		first := ownerBinding("evicted-first")
		firstCapture := ownerCapture(first, 2)
		firstCapture.Messages[len(firstCapture.Messages)-1].Content += strings.Repeat("e", 70<<10)
		firstLatest, err := builder.BuildSubscribeSnapshot(ownerBase(first), firstCapture, 1, "lease-eviction")
		if err != nil {
			t.Fatal(err)
		}
		if len(firstLatest.Externalized) == 0 {
			t.Fatal("eviction setup did not create snapshot-owned content")
		}
		firstRef := firstLatest.Externalized[0].ContentRef
		second := ownerBinding("evicted-second")
		secondLatest, err := builder.BuildSubscribeSnapshot(ownerBase(second), ownerCapture(second, 2), 1, "lease-eviction")
		if err != nil {
			t.Fatal(err)
		}
		if histories.Valid(first) || !histories.Valid(second) {
			t.Fatalf("capacity state first=%v second=%v", histories.Valid(first), histories.Valid(second))
		}
		_, err = builder.BuildOlderHistory(first, firstLatest.History.NextCursor, 1, "lease-eviction")
		requireRemoteCode(t, err, protocol.ErrSnapshotExpired)
		if historyReleased, refsReleased := builder.Release(first); historyReleased || refsReleased == 0 {
			t.Fatalf("evicted Release = history:%v refs:%d", historyReleased, refsReleased)
		}
		_, err = contents.ReadForLease("lease-eviction", protocol.SessionContentParams{ContentRef: firstRef})
		requireRemoteCode(t, err, protocol.ErrContentRefExpired)

		mismatch := second
		mismatch.RuntimeEpoch = "runtime-mismatch"
		_, err = builder.BuildOlderHistory(mismatch, secondLatest.History.NextCursor, 1, "lease-eviction")
		requireRemoteCode(t, err, protocol.ErrSnapshotExpired)
		if !histories.Valid(second) {
			t.Fatal("binding mismatch invalidated the actual snapshot")
		}
	})

	t.Run("closed history", func(t *testing.T) {
		builder, histories, _ := newOwnerBuilder(t, history.Options{}, contentref.Config{})
		binding := ownerBinding("closed")
		latest, err := builder.BuildSubscribeSnapshot(ownerBase(binding), ownerCapture(binding, 2), 1, "lease-closed")
		if err != nil {
			t.Fatal(err)
		}
		histories.Close()
		_, err = builder.BuildOlderHistory(binding, latest.History.NextCursor, 1, "lease-closed")
		requireRemoteCode(t, err, protocol.ErrSnapshotExpired)
	})
}

func TestBuildFailuresReleaseNewHistoryAndContentOwner(t *testing.T) {
	t.Run("outer owner budget", func(t *testing.T) {
		builder, histories, contents := newOwnerBuilder(t, history.Options{}, contentref.Config{})
		binding := ownerBinding("owner-budget")
		base := ownerBase(binding)
		base.Checkpoints = []protocol.CheckpointView{{
			CheckpointID: "checkpoint-too-large",
			Files:        []string{strings.Repeat("f", protocol.SnapshotHistoryBytes+1024)},
		}}
		_, err := builder.BuildSubscribeSnapshot(base, ownerCapture(binding, 2), 2, "lease-owner-budget")
		if !errors.Is(err, history.ErrPageBudget) {
			t.Fatalf("error = %v, want ErrPageBudget", err)
		}
		if histories.Valid(binding) {
			t.Fatal("owner-budget failure retained history")
		}
		if stats := contents.Stats(); stats.Entries != 0 || stats.Owners != 0 || stats.Bytes != 0 {
			t.Fatalf("owner-budget failure retained content: %+v", stats)
		}
	})

	t.Run("content capacity is terminal", func(t *testing.T) {
		builder, histories, contents := newOwnerBuilder(t, history.Options{}, contentref.Config{MaxBytes: 1024})
		binding := ownerBinding("content-capacity")
		capture := ownerCapture(binding, 2)
		// The oldest turn needs external storage while the newest turn fits.
		// Treating ErrCapacity like ErrOwnerBudget would incorrectly shrink to
		// the newest turn and succeed.
		capture.Messages[2].Content = strings.Repeat("x", 70<<10)
		_, err := builder.BuildSubscribeSnapshot(ownerBase(binding), capture, 2, "lease-content-capacity")
		if !errors.Is(err, contentref.ErrCapacity) {
			t.Fatalf("error = %v, want ErrCapacity", err)
		}
		if histories.Valid(binding) {
			t.Fatal("content-capacity failure retained history")
		}
		if stats := contents.Stats(); stats.Entries != 0 || stats.Owners != 0 || stats.Bytes != 0 {
			t.Fatalf("content-capacity failure retained content: %+v", stats)
		}
	})
}

func TestDuplicateCaptureFailureDoesNotReleaseExistingOwner(t *testing.T) {
	builder, histories, contents := newOwnerBuilder(t, history.Options{}, contentref.Config{})
	binding := ownerBinding("duplicate")
	capture := ownerCapture(binding, 1)
	capture.Messages[len(capture.Messages)-1].Content += strings.Repeat("d", 70<<10)
	first, err := builder.BuildSubscribeSnapshot(ownerBase(binding), capture, 1, "lease-duplicate")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Externalized) == 0 {
		t.Fatal("duplicate setup did not create content")
	}
	ref := first.Externalized[0].ContentRef
	if _, err := builder.BuildSubscribeSnapshot(ownerBase(binding), capture, 1, "lease-duplicate"); !errors.Is(err, history.ErrSnapshotExists) {
		t.Fatalf("duplicate error = %v, want ErrSnapshotExists", err)
	}
	if !histories.Valid(binding) {
		t.Fatal("duplicate build released existing history")
	}
	if _, err := contents.ReadForLease("lease-duplicate", protocol.SessionContentParams{ContentRef: ref}); err != nil {
		t.Fatalf("duplicate build released existing content owner: %v", err)
	}
}

func TestBuilderRejectsNilStoresInvalidIdentityLeaseAndPageTurns(t *testing.T) {
	histories, err := history.New(history.Options{SweepInterval: -1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(histories.Close)
	contents, err := contentref.New(testHostEpoch, contentref.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(contents.Close)
	if _, err := New(nil, contents); !errors.Is(err, ErrNilStore) {
		t.Fatalf("nil history constructor error = %v", err)
	}
	if _, err := New(histories, nil); !errors.Is(err, ErrNilStore) {
		t.Fatalf("nil content constructor error = %v", err)
	}
	var nilBuilder *Builder
	if _, err := nilBuilder.BuildOlderHistory(ownerBinding("nil"), "cursor", 1, "lease"); !errors.Is(err, ErrNilStore) {
		t.Fatalf("nil receiver error = %v", err)
	}

	builder, err := New(histories, contents)
	if err != nil {
		t.Fatal(err)
	}
	binding := ownerBinding("validation")
	base := ownerBase(binding)
	if err := base.Validate(); err != nil {
		t.Fatalf("empty History placeholder is not a valid base: %v", err)
	}
	if _, err := builder.BuildSubscribeSnapshot(base, ownerCapture(binding, 1), 0, "lease"); !errors.Is(err, history.ErrInvalidPageTurns) {
		t.Fatalf("pageTurns error = %v", err)
	}
	if _, err := builder.BuildSubscribeSnapshot(base, ownerCapture(binding, 1), 1, "   "); !errors.Is(err, contentref.ErrInvalidLease) {
		t.Fatalf("lease error = %v", err)
	}
	mismatches := []struct {
		name   string
		mutate func(*protocol.SessionSnapshot)
	}{
		{name: "snapshot", mutate: func(value *protocol.SessionSnapshot) { value.SnapshotID = "snapshot-other" }},
		{name: "host", mutate: func(value *protocol.SessionSnapshot) { value.HostEpoch = "host-other" }},
		{name: "target", mutate: func(value *protocol.SessionSnapshot) { value.Target.SessionID = "session-other" }},
		{name: "runtime", mutate: func(value *protocol.SessionSnapshot) { value.RuntimeEpoch = "runtime-other" }},
		{name: "history owner", mutate: func(value *protocol.SessionSnapshot) { value.History.SnapshotID = "snapshot-other" }},
	}
	for _, test := range mismatches {
		t.Run(test.name, func(t *testing.T) {
			mismatch := base
			test.mutate(&mismatch)
			if _, err := builder.BuildSubscribeSnapshot(mismatch, ownerCapture(binding, 1), 1, "lease"); !errors.Is(err, ErrBindingMismatch) {
				t.Fatalf("binding mismatch error = %v", err)
			}
		})
	}
	alreadyExternalized := base
	alreadyExternalized.Externalized = []protocol.ExternalizedField{{JSONPointer: "/meta/goal"}}
	if _, err := builder.BuildSubscribeSnapshot(alreadyExternalized, ownerCapture(binding, 1), 1, "lease"); !errors.Is(err, contentref.ErrAlreadyExternalized) {
		t.Fatalf("already-externalized base error = %v", err)
	}
	if stats := histories.Stats(); stats.Snapshots != 0 {
		t.Fatalf("pre-publication validation retained history: %+v", stats)
	}
	if _, err := builder.BuildOlderHistory(binding, "", 1, "lease"); !errors.Is(err, history.ErrInvalidCapture) {
		t.Fatalf("empty cursor error = %v", err)
	}

	wrongEpoch := binding
	wrongEpoch.HostEpoch = "host-other"
	if released, refs := builder.Release(wrongEpoch); released || refs != 0 {
		t.Fatalf("cross-epoch Release = %v, %d", released, refs)
	}
}

func TestConcurrentOlderBuildsRemainRaceSafe(t *testing.T) {
	builder, _, contents := newOwnerBuilder(t, history.Options{}, contentref.Config{})
	binding := ownerBinding("concurrent")
	capture := ownerCapture(binding, 6)
	for index := range capture.Messages {
		if capture.Messages[index].Role == provider.RoleAssistant {
			capture.Messages[index].Content += strings.Repeat("z", 70<<10)
		}
	}
	latest, err := builder.BuildSubscribeSnapshot(ownerBase(binding), capture, 2, "lease-concurrent")
	if err != nil {
		t.Fatal(err)
	}

	const workers = 16
	var wg sync.WaitGroup
	errorsCh := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, buildErr := builder.BuildOlderHistory(binding, latest.History.NextCursor, 2, "lease-concurrent")
			if buildErr != nil {
				errorsCh <- buildErr
				return
			}
			if len(page.Externalized) == 0 {
				errorsCh <- errors.New("concurrent older page has no descriptors")
				return
			}
			_, readErr := contents.ReadForLease("lease-concurrent", protocol.SessionContentParams{ContentRef: page.Externalized[0].ContentRef})
			if readErr != nil {
				errorsCh <- readErr
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	builder.Release(binding)
}
