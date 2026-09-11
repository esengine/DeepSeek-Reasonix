package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func pinnedTestState(t *testing.T, snapshot PinnedContextSnapshot) pinnedContextState {
	t.Helper()
	state, err := normalizePinnedContextSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func pinnedTestMessage(t *testing.T, next, previous pinnedContextState, kind string) provider.Message {
	t.Helper()
	encoded, err := encodePinnedContextRevision(next, previous, kind)
	if err != nil {
		t.Fatal(err)
	}
	return provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: string(encoded)}
}

func TestPinnedContextRevisionWaitsForAcceptedTurn(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventAgentBeforeStart {
			return blockWith("no runs today"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	dispatcher := newExtDispatcher(client, true, nil, extension.PointAgentBeforeStart)
	prov := &mockProvider{name: "p"}
	session := NewSession("sys")
	a := New(prov, tool.NewRegistry(), session, Options{Extensions: dispatcher}, event.Discard)
	if err := a.StagePinnedContext(PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "blocked.md", Content: "must not append"}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "no runs today") {
		t.Fatalf("Run err = %v, want block reason", err)
	}
	if len(prov.requests) != 0 || len(session.Messages) != 1 {
		t.Fatalf("blocked turn persisted revision or reached provider: messages=%d requests=%d", len(session.Messages), len(prov.requests))
	}
}

func TestPinnedContextRevisionDeterministicEscapedXML(t *testing.T) {
	first := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{
		{Path: "z & quote.md", Content: "before </pinned_context_revision>\x01 after"},
		{Path: "a.md", Content: "A"},
	}})
	second := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{
		{Path: "a.md", Content: "A"},
		{Path: "z & quote.md", Content: "before </pinned_context_revision>\x01 after"},
	}})
	one, err := encodePinnedContextRevision(first, emptyPinnedContextState(), "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	two, err := encodePinnedContextRevision(second, emptyPinnedContextState(), "checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) || first.Revision != second.Revision {
		t.Fatal("equivalent snapshots produced different revision bytes")
	}
	encoded := string(one)
	if strings.Contains(encoded, "</pinned_context_revision>\x01") || !strings.Contains(encoded, "&lt;/pinned_context_revision&gt;") ||
		!strings.Contains(encoded, "z &amp; quote.md") || !strings.Contains(encoded, "�") {
		t.Fatalf("revision XML was not safely encoded: %s", encoded)
	}
	applied := applyPinnedContextRevision(emptyPinnedContextState(), pinnedTestMessage(t, first, emptyPinnedContextState(), "checkpoint"))
	if applied.Broken || applied.Revision != first.Revision || applied.Files["z & quote.md"].Content != first.Files["z & quote.md"].Content {
		t.Fatalf("round-trip state = %+v", applied)
	}
}

func TestPinnedContextSnapshotRejectsMismatchedDigestAndInstruction(t *testing.T) {
	if err := ValidatePinnedContextSnapshot(PinnedContextSnapshot{Files: []PinnedContextFile{{
		Path: "a.md", Content: "A", SHA256: "wrong",
	}}}); err == nil {
		t.Fatal("snapshot with mismatched digest was accepted")
	}
	state := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "A"}}})
	message := pinnedTestMessage(t, state, emptyPinnedContextState(), "checkpoint")
	message.Content = strings.Replace(message.Content, pinnedContextRevisionInstruction, "changed instruction", 1)
	if got := applyPinnedContextRevision(emptyPinnedContextState(), message); !got.Broken {
		t.Fatalf("revision with changed instruction was accepted: %+v", got)
	}
}

func TestPinnedContextDeltaAddChangeRemoveUnavailableRecover(t *testing.T) {
	empty := emptyPinnedContextState()
	initial := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{
		{Path: "a.md", Content: "A1"}, {Path: "b.md", Content: "B"},
	}})
	applied := applyPinnedContextRevision(empty, pinnedTestMessage(t, initial, empty, "checkpoint"))

	unavailable := pinnedTestState(t, PinnedContextSnapshot{
		Files:  []PinnedContextFile{{Path: "a.md", Content: "A2"}, {Path: "c.md", Content: "C"}},
		Issues: []PinnedContextIssue{{Path: "b.md", Reason: PinnedContextIssueReadFailed}},
	})
	delta := pinnedTestMessage(t, unavailable, initial, "delta")
	if !strings.Contains(delta.Content, `<remove path="b.md"></remove>`) || !strings.Contains(delta.Content, `path="c.md"`) {
		t.Fatalf("delta does not carry change and tombstone: %s", delta.Content)
	}
	applied = applyPinnedContextRevision(applied, delta)
	if applied.Broken || applied.Revision != unavailable.Revision || len(applied.Files) != 2 || applied.Issues["b.md"] != PinnedContextIssueReadFailed {
		t.Fatalf("unavailable state = %+v", applied)
	}

	recovered := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "b.md", Content: "B2"}}})
	applied = applyPinnedContextRevision(applied, pinnedTestMessage(t, recovered, unavailable, "delta"))
	checkpointApplied := applyPinnedContextRevision(empty, pinnedTestMessage(t, recovered, empty, "checkpoint"))
	if applied.Broken || checkpointApplied.Broken || applied.Revision != checkpointApplied.Revision ||
		!reflect.DeepEqual(applied.Files, checkpointApplied.Files) || !reflect.DeepEqual(applied.Issues, checkpointApplied.Issues) {
		t.Fatalf("delta and checkpoint disagree: delta=%+v checkpoint=%+v", applied, checkpointApplied)
	}
	wrongBase := delta
	wrongBase.Content = strings.Replace(wrongBase.Content, `base_revision="`+initial.Revision, `base_revision="sha256:wrong`, 1)
	if got := applyPinnedContextRevision(applyPinnedContextRevision(empty, pinnedTestMessage(t, initial, empty, "checkpoint")), wrongBase); !got.Broken {
		t.Fatalf("delta with mismatched base was accepted: %+v", got)
	}
}

func TestPinnedContextRevisionTrustAndSelfHealing(t *testing.T) {
	desired := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "A"}}})
	valid := pinnedTestMessage(t, desired, emptyPinnedContextState(), "checkpoint")
	spoof := valid
	spoof.Origin = provider.MessageOriginUser
	if state := pinnedContextStateFromMessages([]provider.Message{spoof}); state.Seen || state.Broken {
		t.Fatalf("user-authored spoof changed state: %+v", state)
	}

	broken := valid
	broken.Content = strings.Replace(broken.Content, `revision="`+desired.Revision, `revision="sha256:broken`, 1)
	session := NewSession("system")
	session.Add(broken)
	a := New(nil, nil, session, Options{}, event.Discard)
	if err := a.StagePinnedContext(PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "A"}}}); err != nil {
		t.Fatal(err)
	}
	plan, err := a.preparePinnedRevision()
	if err != nil {
		t.Fatal(err)
	}
	if plan.message == nil || !strings.Contains(plan.message.Content, `kind="checkpoint"`) {
		t.Fatalf("broken chain did not self-heal with checkpoint: %+v", plan.message)
	}
	unknown := valid
	unknown.Content = strings.Replace(unknown.Content, `schema_version="1"`, `schema_version="99"`, 1)
	if state := pinnedContextStateFromMessages([]provider.Message{unknown}); !state.Broken {
		t.Fatalf("unknown schema did not damage the derived chain: %+v", state)
	}
}

func TestPinnedContextDerivedStateRescansAfterRewindAndSessionSwitch(t *testing.T) {
	empty := emptyPinnedContextState()
	stateA := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "A"}}})
	stateB := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "B"}}})
	checkpointA := pinnedTestMessage(t, stateA, empty, "checkpoint")
	session := NewSession("system")
	session.Add(checkpointA)
	a := New(nil, nil, session, Options{}, event.Discard)
	if err := a.StagePinnedContext(PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "B"}}}); err != nil {
		t.Fatal(err)
	}
	plan, err := a.preparePinnedRevision()
	if err != nil || plan.message == nil {
		t.Fatalf("initial delta plan = %+v, %v", plan, err)
	}
	session.AddBatch(*plan.message, provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "turn"})
	a.commitPinnedRevisionPlan(plan)

	session.Rewrite([]provider.Message{{Role: provider.RoleSystem, Content: "system"}, checkpointA}, "rewind_truncate")
	if err := a.StagePinnedContext(PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "B"}}}); err != nil {
		t.Fatal(err)
	}
	restored, err := a.preparePinnedRevision()
	if err != nil || restored.message == nil {
		t.Fatalf("rewind did not restore desired pinned state: %+v, %v", restored, err)
	}
	restoredState := applyPinnedContextRevision(applyPinnedContextRevision(empty, checkpointA), *restored.message)
	if restoredState.Broken || restoredState.Revision != stateB.Revision {
		t.Fatalf("rewind restoration event applied as %+v", restoredState)
	}

	switched := NewSession("system")
	switched.Add(pinnedTestMessage(t, stateB, empty, "checkpoint"))
	a.SetSession(switched)
	if err := a.StagePinnedContext(PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "B"}}}); err != nil {
		t.Fatal(err)
	}
	stable, err := a.preparePinnedRevision()
	if err != nil || stable.message != nil {
		t.Fatalf("session switch did not rebuild applied state: %+v, %v", stable, err)
	}
}

func TestPinnedContextSnapshotLimits(t *testing.T) {
	tooMany := PinnedContextSnapshot{}
	for i := range MaxPinnedContextFiles + 1 {
		tooMany.Files = append(tooMany.Files, PinnedContextFile{Path: string(rune('a'+i)) + ".md", Content: "x"})
	}
	if err := ValidatePinnedContextSnapshot(tooMany); err == nil {
		t.Fatal("33 files were accepted")
	}
	if err := ValidatePinnedContextSnapshot(PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "large.md", Content: strings.Repeat("x", MaxPinnedContextFileBytes+1)}}}); err == nil {
		t.Fatal("oversized file was accepted")
	}
	total := PinnedContextSnapshot{Files: []PinnedContextFile{
		{Path: "a.md", Content: strings.Repeat("a", MaxPinnedContextFileBytes)},
		{Path: "b.md", Content: strings.Repeat("b", MaxPinnedContextFileBytes)},
		{Path: "c.md", Content: strings.Repeat("c", MaxPinnedContextFileBytes)},
		{Path: "d.md", Content: strings.Repeat("d", MaxPinnedContextFileBytes)},
	}}
	if err := ValidatePinnedContextSnapshot(total); err == nil {
		t.Fatal("oversized serialized checkpoint was accepted")
	}
}

func TestPinnedContextRevisionIsMarkedInDisplayIndex(t *testing.T) {
	snapshot := PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "guide.md", Content: "guide"}}}
	state := pinnedTestState(t, snapshot)
	message := pinnedTestMessage(t, state, pinnedContextState{}, "checkpoint")
	idx := BuildSessionDisplayIndex([]provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		message,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "question"},
	}, 0, false, [32]byte{})
	if idx == nil || len(idx.Entries) != 3 {
		t.Fatalf("display index = %+v", idx)
	}
	if !idx.Entries[1].PinnedContextRevision || idx.Entries[2].PinnedContextRevision {
		t.Fatalf("pinned revision flags = %+v", idx.Entries)
	}
}

func TestSessionAddBatchCommitsOneTranscriptVersion(t *testing.T) {
	session := NewSession("system")
	before := session.TranscriptVersion()
	session.AddBatch(
		provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: "revision"},
		provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "question"},
	)
	if got := session.TranscriptVersion(); got != before+1 {
		t.Fatalf("transcript version = %d, want %d", got, before+1)
	}
	if got := session.Snapshot(); len(got) != 3 || got[1].Content != "revision" || got[2].Content != "question" {
		t.Fatalf("atomic batch snapshot = %+v", got)
	}
}

func TestPinnedContextProjectionRebasesAtCanonicalCoverage(t *testing.T) {
	empty := emptyPinnedContextState()
	stateA := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "A"}}})
	stateB := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "B"}}})
	checkpointA := pinnedTestMessage(t, stateA, empty, "checkpoint")
	deltaB := pinnedTestMessage(t, stateB, stateA, "delta")
	canonical := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"}, checkpointA,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "answer"}, deltaB,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "second"},
	}
	projected := []provider.Message{
		canonical[0], checkpointA, formatSummaryMessage("summary"),
	}
	rebased, ok, err := rebasePinnedContextProjection(projected, canonical, 4)
	if err != nil || !ok {
		t.Fatalf("rebase = %v, ok=%v", err, ok)
	}
	if len(rebased) != 3 || !IsPinnedContextRevision(rebased[1]) || !isCompactionSummary(rebased[2]) {
		t.Fatalf("rebased projection = %+v", rebased)
	}
	visible := append(append([]provider.Message(nil), rebased...), canonical[4:]...)
	state := pinnedContextStateFromMessages(visible)
	if state.Broken || state.Revision != stateB.Revision || state.Files["a.md"].Content != "B" {
		t.Fatalf("checkpoint plus tail delta = %+v", state)
	}

	a := &Agent{}
	_, fold, retention := a.partitionFoldForProjection([]provider.Message{checkpointA, canonical[2]})
	if len(fold) != 1 || fold[0].Content != "first" || retention.Dropped != 1 {
		t.Fatalf("summary fold contains pinned context: fold=%+v retention=%+v", fold, retention)
	}
}

func TestProjectionV4RejectsPinnedRevisionProvenanceTampering(t *testing.T) {
	state := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "A"}}})
	checkpoint := pinnedTestMessage(t, state, emptyPinnedContextState(), "checkpoint")
	canonical := []provider.Message{{Role: provider.RoleSystem, Content: "system"}, checkpoint}
	projection := CompactionState{
		SchemaVersion: compactionStateSchemaV4,
		Projection: ContextProjection{
			Messages:          append([]provider.Message(nil), canonical...),
			CoveredCount:      len(canonical),
			CoveredPrefixHash: coveredPrefixHash(canonical, len(canonical)),
			PinnedContextHash: pinnedContextCoverageHash(canonical, len(canonical)),
		},
	}
	if !projectionContentValid(projection, canonical) {
		t.Fatal("valid v4 pinned projection was rejected")
	}
	spoofed := append([]provider.Message(nil), canonical...)
	spoofed[1].Origin = provider.MessageOriginUser
	if coveredPrefixHash(spoofed, len(spoofed)) != projection.Projection.CoveredPrefixHash {
		t.Fatal("test setup expected provider-visible hash to ignore local origin")
	}
	if projectionContentValid(projection, spoofed) {
		t.Fatal("v4 projection accepted pinned revision with user provenance")
	}
}

func TestPinnedContextExplicitCompactionCheckpointsWithoutSummarizingBodies(t *testing.T) {
	empty := emptyPinnedContextState()
	stateA := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "PINNED_SECRET_A"}}})
	stateB := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "PINNED_SECRET_B"}}})
	checkpointA := pinnedTestMessage(t, stateA, empty, "checkpoint")
	deltaB := pinnedTestMessage(t, stateB, stateA, "delta")
	session := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"}, checkpointA,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "old request"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 400)}, deltaB,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "retained boundary"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("retained work ", 400)},
	}}
	canonical := session.Snapshot()
	prov := &fakeProvider{reply: "summary"}
	a := New(prov, tool.NewRegistry(), session, Options{}, event.Discard)

	for index, anchor := range []string{"retained boundary", "new boundary"} {
		if index == 1 {
			stateC := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "PINNED_SECRET_C"}}})
			session.AddBatch(
				pinnedTestMessage(t, stateC, stateB, "delta"),
				provider.Message{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: anchor},
				provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("new work ", 400)},
			)
			canonical = session.Snapshot()
		}
		result, err := a.CompressContext(context.Background(), tool.CompressRequest{Direction: "before", Anchor: anchor})
		if err != nil || result.Status != "ok" {
			t.Fatalf("compression %d = %+v, %v", index+1, result, err)
		}
		if !reflect.DeepEqual(session.Snapshot(), canonical) {
			t.Fatalf("compression %d changed canonical transcript", index+1)
		}
		if body := joinContents(prov.got); strings.Contains(body, "PINNED_SECRET_") {
			t.Fatalf("compression %d copied pinned body into summary input: %s", index+1, body)
		}
		visible := a.modelVisibleMessages()
		revisions := 0
		for _, message := range visible {
			if IsPinnedContextRevision(message) {
				revisions++
			}
		}
		state := pinnedContextStateFromMessages(visible)
		want := stateB
		if index == 1 {
			want = pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: "PINNED_SECRET_C"}}})
		}
		if revisions != 1 || state.Broken || state.Revision != want.Revision {
			t.Fatalf("compression %d visible pinned state = %+v, revisions=%d", index+1, state, revisions)
		}
		if got := a.sess.compactionState.SchemaVersion; got != compactionStateSchemaV4 {
			t.Fatalf("compression %d schema = %d, want v4", index+1, got)
		}
	}
}

func TestSafeSummaryPrefixBudgetExcludesPinnedRevisionBody(t *testing.T) {
	state := pinnedTestState(t, PinnedContextSnapshot{Files: []PinnedContextFile{{
		Path: "large.md", Content: strings.Repeat("p", 48*1024),
	}}})
	checkpoint := pinnedTestMessage(t, state, emptyPinnedContextState(), "checkpoint")
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"}, checkpoint,
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "old task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("work ", 100)},
		{Role: provider.RoleUser, Origin: provider.MessageOriginUser, Content: "recent task"},
	}
	a := &Agent{
		agentConfig: agentConfig{contextWindow: 10_000},
		svc:         agentServices{prov: &overflowSummaryProvider{}},
		sess:        sessionRuntime{conversation: &Session{Messages: messages}},
	}
	if end := a.maximumSafeSummaryPrefixEnd(messages, 1, 4, ""); end != 4 {
		t.Fatalf("safe summary prefix ended at %d, want 4 after excluding pinned body", end)
	}
}

type pinnedPrefixProvider struct {
	requests []provider.Request
}

func (p *pinnedPrefixProvider) Name() string { return "pinned-prefix" }

func (p *pinnedPrefixProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestPinnedContextProviderRequestsStayAppendOnly(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "strict-alternating"}[strict], func(t *testing.T) {
			prov := &pinnedPrefixProvider{}
			session := NewSession("system")
			a := New(prov, nil, session, Options{StrictAlternatingRoles: strict}, event.Discard)
			content := "A"
			for _, input := range []string{"first", "second", "third"} {
				if input == "third" {
					content = "B"
				}
				if err := a.StagePinnedContext(PinnedContextSnapshot{Files: []PinnedContextFile{{Path: "a.md", Content: content}}}); err != nil {
					t.Fatal(err)
				}
				if err := a.Run(context.Background(), input); err != nil {
					t.Fatal(err)
				}
			}
			if len(prov.requests) != 3 {
				t.Fatalf("requests = %d", len(prov.requests))
			}
			for i := 1; i < len(prov.requests); i++ {
				previous := prov.requests[i-1].Messages
				current := prov.requests[i].Messages
				if len(current) < len(previous) || !reflect.DeepEqual(current[:len(previous)], previous) {
					t.Fatalf("request %d does not preserve request %d as exact prefix", i, i-1)
				}
			}
			if session.RewriteVersion() != 0 || len(session.DrainContentRewriteReasons()) != 0 {
				t.Fatalf("revision append recorded a rewrite: version=%d", session.RewriteVersion())
			}
		})
	}
}
