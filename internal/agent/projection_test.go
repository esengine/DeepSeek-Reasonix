package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestCompactionStateAtomicSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	st := CompactionState{
		SchemaVersion:     compactionStateSchemaV1,
		TranscriptVersion: 3,
		Projection: ContextProjection{
			Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "sys"},
				{Role: provider.RoleUser, Content: "summary-body"},
			},
			TranscriptVersion: 3,
			ProjectionVersion: 1,
			CoveredCount:      10,
			SummaryHash:       summaryContentHash("summary-body"),
			SourceTokens:      1000,
			ProjectionTokens:  200,
		},
		PromptCacheKey: "ws|sess|model",
		LastCacheState: CacheStateCold,
		LastTrigger:    CompactionTriggerPressure,
		LastMode:       CompactionModeSummarized,
	}
	if err := SaveCompactionState(path, st); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok, err := LoadCompactionState(path)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if got.TranscriptVersion != 3 || got.LastMode != CompactionModeSummarized {
		t.Fatalf("loaded state = %+v", got)
	}
	if len(got.Projection.Messages) != 2 || got.Projection.CoveredCount != 10 {
		t.Fatalf("projection = %+v", got.Projection)
	}
}

func TestCompactToProjectionLeavesCanonicalIntact(t *testing.T) {
	fp := &fakeProvider{reply: "GOAL: ship projection\nFACTS: keep path /tmp/x"}
	sess := NewSession("sys")
	// Build enough history that a fold is economical.
	for i := range 12 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "user turn " + strings.Repeat("x", 80) + " " + string(rune('A'+i%26))})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "assistant work " + strings.Repeat("y", 200)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c" + string(rune('0'+i%10)), Name: "read", Arguments: `{"path":"f"}`}}})
		sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c" + string(rune('0'+i%10)), Name: "read", Content: strings.Repeat("tool-out-", 40)})
	}
	before := append([]provider.Message(nil), sess.Messages...)
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	a := New(fp, nil, sess, Options{
		ContextWindow: 2000,
		RecentKeep:    2,
		ArchiveDir:    dir,
		SessionPath:   sessionPath,
		ModelRef:      "test/model",
	}, event.Discard)

	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	after := sess.Snapshot()
	if len(after) != len(before) {
		t.Fatalf("canonical length changed: before=%d after=%d", len(before), len(after))
	}
	for i := range before {
		if before[i].Content != after[i].Content || before[i].Role != after[i].Role {
			t.Fatalf("canonical message %d changed", i)
		}
	}
	if len(a.compactionState.Projection.Messages) == 0 {
		t.Fatal("expected projection messages")
	}
	// Projection must be shorter than canonical.
	if estimateMessagesTokens(a.compactionState.Projection.Messages) >= estimateMessagesTokens(before) {
		t.Fatalf("projection did not shrink: proj=%d src=%d",
			estimateMessagesTokens(a.compactionState.Projection.Messages),
			estimateMessagesTokens(before))
	}
	// Sidecar must exist and reload.
	st, ok, err := LoadCompactionState(sessionPath)
	if err != nil || !ok {
		t.Fatalf("reload sidecar: ok=%v err=%v", ok, err)
	}
	if st.LastMode != CompactionModeSummarized {
		t.Fatalf("mode = %q, want summarized", st.LastMode)
	}
	// Model-visible must use projection.
	visible := a.modelVisibleMessages()
	if len(visible) == len(before) {
		t.Fatal("model-visible still full canonical")
	}
	// Summarizer must have been invoked with fold region (no tools schema).
	if len(fp.got) == 0 {
		t.Fatal("summarizer was not called")
	}
}

func TestCompactFailureDoesNotWriteMechanicalMarker(t *testing.T) {
	fp := &fakeProvider{streamErr: errors.New("boom")}
	sess := NewSession("sys")
	for range 10 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("u", 100)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 200)})
	}
	before := append([]provider.Message(nil), sess.Messages...)
	a := New(fp, nil, sess, Options{ContextWindow: 2000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	err := a.CompactNow(context.Background(), "")
	if err == nil {
		t.Fatal("expected compaction error")
	}
	after := sess.Snapshot()
	if len(after) != len(before) {
		t.Fatalf("canonical changed on failure: %d → %d", len(before), len(after))
	}
	for _, m := range after {
		if strings.Contains(m.Content, "summary was unavailable") || strings.Contains(m.Content, "folded here to free context") {
			t.Fatalf("mechanical marker written into history: %q", m.Content)
		}
	}
	if len(a.compactionState.Projection.Messages) != 0 {
		t.Fatal("failed compaction installed a projection")
	}
}

func TestFixedEarlyUserTurnsStableAcrossCompactions(t *testing.T) {
	fp := &fakeProvider{reply: "digest-1"}
	sess := NewSession("sys")
	// 30 distinct user turns so a "latest N" strategy would reshuffle. The
	// first four are large enough that usage-calibrated eligibility would reject
	// them at 1 token/char, but the fixed fallback estimate accepts them.
	for i := range 30 {
		user := "unique-user-fact-" + strings.Repeat(string(rune('a'+i%26)), 20) + "-" + strings.Repeat("0", i%10+1)
		if i < 4 {
			user = "fixed-early-" + string(rune('a'+i)) + strings.Repeat("x", 1200)
		}
		sess.Add(provider.Message{Role: provider.RoleUser, Content: user})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("work-", 50) + string(rune('A'+i%26))})
	}
	dir := t.TempDir()
	a := New(fp, nil, sess, Options{ContextWindow: 4000, RecentKeep: 2, ArchiveDir: dir, SessionPath: filepath.Join(dir, "s.jsonl")}, event.Discard)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("compact1: %v", err)
	}
	firstPrefix := earlyUserPrefix(a.compactionState.Projection.Messages)
	// Grow the session and compact again.
	for i := range 8 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "later-fact-" + strings.Repeat("z", 30) + string(rune('0'+i))})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("more-", 60)})
	}
	// Simulate a projected request reporting a very different calibration from
	// the pre-projection canonical estimate. This remains useful for tail sizing,
	// but must not change which early turns define the stable prefix.
	a.lastUsage.Store(&provider.Usage{PromptTokens: charsOfMessages(sess.Messages)})
	if got := a.tokPerChar(); got < 0.9 || got > 1.1 {
		t.Fatalf("test did not install the intended dynamic calibration: %f", got)
	}
	fp.reply = "digest-2"
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("compact2: %v", err)
	}
	secondPrefix := earlyUserPrefix(a.compactionState.Projection.Messages)
	if firstPrefix != secondPrefix {
		t.Fatalf("early user prefix drifted across compactions:\n1: %q\n2: %q", firstPrefix, secondPrefix)
	}
	// Exactly one summary in the projection (A1 rolling merge).
	summaries := 0
	for _, m := range a.compactionState.Projection.Messages {
		if isCompactionSummary(m) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("summaries in projection = %d, want 1", summaries)
	}
}

func earlyUserPrefix(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			continue
		}
		if isCompactionSummary(m) {
			break
		}
		if m.Role == provider.RoleUser {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestLocalOnlyExcludedFromCompactionRequest(t *testing.T) {
	fp := &fakeProvider{reply: "ok"}
	sess := NewSession("sys")
	for range 8 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("u", 80)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 120)})
	}
	sess.Add(provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		Content: "secret local only", LocalOnly: true,
	})
	a := New(fp, nil, sess, Options{ContextWindow: 2000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("compact: %v", err)
	}
	for _, m := range fp.got {
		if strings.Contains(m.Content, "secret local only") {
			t.Fatal("LocalOnly content reached summarizer")
		}
	}
	for _, m := range a.compactionState.Projection.Messages {
		if m.LocalOnly || strings.Contains(m.Content, "secret local only") {
			t.Fatal("LocalOnly content entered projection")
		}
	}
}

func TestArchiveFailureLeavesNoProjection(t *testing.T) {
	fp := &fakeProvider{reply: "digest"}
	sess := NewSession("sys")
	for range 8 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("u", 80)})
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a", 120)})
	}
	// Point archive at a file path so MkdirAll/Create fails.
	badArchive := filepath.Join(t.TempDir(), "not-a-dir")
	if err := writeFile(badArchive, []byte("x")); err != nil {
		t.Fatal(err)
	}
	a := New(fp, nil, sess, Options{ContextWindow: 2000, RecentKeep: 2, ArchiveDir: badArchive}, event.Discard)
	if err := a.CompactNow(context.Background(), ""); err == nil {
		t.Fatal("expected archive failure")
	}
	if len(a.compactionState.Projection.Messages) != 0 {
		t.Fatal("half-finished projection after archive failure")
	}
}

func writeFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o644)
}

func visibleContext(a *Agent) []provider.Message {
	if a == nil {
		return nil
	}
	if msgs := a.compactionState.Projection.Messages; len(msgs) > 0 {
		return msgs
	}
	if a.session != nil {
		return a.session.Snapshot()
	}
	return nil
}

func hasCompactionSummary(msgs []provider.Message) bool {
	return slices.ContainsFunc(msgs, isCompactionSummary)
}

func joinContents(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestCompactReplacesHistory(t *testing.T) {
	prov := &fakeProvider{reply: "- goal: do X\n- changed file Y"}
	bigStep := strings.Repeat("important implementation detail ", 80)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task " + bigStep},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: "file contents"},
		{Role: provider.RoleAssistant, Content: "did a step"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	dir := t.TempDir()
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: dir}, event.Discard)
	beforeLen := len(sess.Messages)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	// Canonical transcript is never rewritten by projection compaction.
	if got := sess.RewriteVersion(); got != 0 {
		t.Fatalf("rewrite version = %d, want 0 (canonical intact)", got)
	}
	if len(sess.Messages) != beforeLen {
		t.Fatalf("canonical len changed: %d -> %d", beforeLen, len(sess.Messages))
	}

	proj := visibleContext(a)
	if !hasCompactionSummary(proj) {
		t.Fatalf("projection missing summary: %+v", proj)
	}
	if proj[0].Role != provider.RoleSystem {
		t.Errorf("message 0 = %s, want system", proj[0].Role)
	}
	// Tail preserved in projection.
	if proj[len(proj)-2].Content != "next" || proj[len(proj)-1].Content != "ok" {
		t.Errorf("recent tail not preserved: %+v", proj[len(proj)-2:])
	}
	var foundSummary bool
	for _, m := range proj {
		if strings.Contains(m.Content, "Summary of earlier") && strings.Contains(m.Content, "do X") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Errorf("summary missing do X: %+v", proj)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive dir: entries=%d err=%v", len(entries), err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines < 1 {
		t.Errorf("archived %d lines, want >=1:\n%s", lines, data)
	}
	if !strings.HasSuffix(entries[0].Name(), ".jsonl") {
		t.Errorf("archive name = %q, want .jsonl", entries[0].Name())
	}
}

func TestCompactFallsBackToMechanicalFoldWhenSummaryFails(t *testing.T) {
	// Projection compaction must not rewrite history or install a mechanical
	// marker when the summarizer fails. The error is returned so the caller
	// can retry or report it.
	prov := &fakeProvider{streamErr: errors.New("provider down")}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: "step one"},
		{Role: provider.RoleUser, Content: "more"},
		{Role: provider.RoleAssistant, Content: "step two"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	var got []event.Event
	sink := event.FuncSink(func(e event.Event) { got = append(got, e) })
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, sink)

	before := append([]provider.Message(nil), sess.Messages...)
	if err := a.compact(context.Background(), "manual", "", true); err == nil {
		t.Fatal("compact should error when summarizer fails")
	}
	if len(sess.Messages) != len(before) {
		t.Fatalf("canonical changed on summarizer failure: %d -> %d", len(before), len(sess.Messages))
	}
	for _, m := range sess.Messages {
		if strings.Contains(m.Content, "summary was unavailable") {
			t.Fatalf("mechanical marker written: %q", m.Content)
		}
	}
	if len(a.compactionState.Projection.Messages) != 0 {
		t.Fatal("failed compact installed a projection")
	}
	// CompactionDone with empty summary resolves the UI placeholder.
	var done *event.Compaction
	for i := range got {
		if got[i].Kind == event.CompactionDone {
			done = &got[i].Compaction
		}
	}
	if done == nil {
		t.Fatal("expected CompactionDone on abort")
	}
}

func TestCompactRewriteVersionFeedsCacheDiagnostics(t *testing.T) {
	prov := &fakeProvider{reply: "- summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "a"},
		{Role: provider.RoleAssistant, Content: "b"},
		{Role: provider.RoleUser, Content: "c"},
		{Role: provider.RoleAssistant, Content: "d"},
		{Role: provider.RoleUser, Content: "e"},
		{Role: provider.RoleAssistant, Content: "f"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)
	before := CaptureShape("sys", nil, sess.RewriteVersion())

	if err := a.compact(context.Background(), "auto", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	// Canonical rewriteVersion stays put; projection notes compact_auto for diagnostics.
	if sess.RewriteVersion() != before.LogRewriteVersion {
		t.Fatalf("canonical rewrite version changed: %d -> %d", before.LogRewriteVersion, sess.RewriteVersion())
	}
	if !hasCompactionSummary(visibleContext(a)) {
		t.Fatal("expected projection summary")
	}
	reasons := sess.DrainContentRewriteReasons()
	// Reasons alone attribute the prefix change; rewrite version need not bump.
	after := CaptureShape("sys", nil, sess.RewriteVersion())
	diag := CompareShape(before, after, &provider.Usage{CacheMissTokens: 10}, reasons)
	if !diag.PrefixChanged {
		t.Fatalf("diagnostics should report prefix change: %+v", diag)
	}
	if len(diag.PrefixChangeReasons) != 1 || diag.PrefixChangeReasons[0] != "compact_auto" {
		t.Fatalf("change reasons = %v, want [compact_auto]", diag.PrefixChangeReasons)
	}
}

func TestCompactKeepsMidSessionUserTurns(t *testing.T) {
	big := strings.Repeat("work output ", 100)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first task"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: big},
		{Role: provider.RoleUser, Content: "by the way, always use pnpm not npm"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleTool, ToolCallID: "2", Name: "read_file", Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	// Summarizer preserves the mid-session fact in the rolling digest.
	a := New(&fakeProvider{reply: "Standing facts: always use pnpm not npm"}, tool.NewRegistry(), sess,
		Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Canonical retains every user turn; projection keeps fixed early turns
	// and folds later small user turns into the single summary.
	var pinnedFirst, keptMidCanonical bool
	for _, m := range sess.Snapshot() {
		if m.Role == provider.RoleUser && m.Content == "first task" {
			pinnedFirst = true
		}
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "always use pnpm not npm") {
			keptMidCanonical = true
		}
	}
	if !pinnedFirst || !keptMidCanonical {
		t.Fatalf("canonical lost user turns (first=%v mid=%v)", pinnedFirst, keptMidCanonical)
	}
	proj := visibleContext(a)
	var projFirst, projMid bool
	for _, m := range proj {
		if isCompactionSummary(m) {
			continue
		}
		if m.Role == provider.RoleUser && m.Content == "first task" {
			projFirst = true
		}
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "always use pnpm not npm") {
			projMid = true
		}
	}
	if !projFirst {
		t.Fatalf("fixed early user turn missing from projection: %+v", proj)
	}
	// The mid-session fact is still among the first few small user turns by
	// position, so the fixed early window keeps it verbatim (stable prefix).
	if !projMid {
		t.Fatalf("early-position user fact missing from projection: %+v", proj)
	}
	if strings.Contains(joinContents(proj), big) {
		t.Errorf("assistant/tool work was not folded out of projection")
	}
}

func TestCompactKeepsPriorDigests(t *testing.T) {
	// A1 rolling merge: prior digests enter the fold and the summarizer must
	// carry durable facts forward into a single latest summary. The fake
	// provider echoes the prior fact so we can assert the fold input included it.
	priorDigest := summaryTagOpen + "\n## Standing facts\n- db is orion_prod_42\n" + summaryTagClose
	big := strings.Repeat("work output ", 200)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: big}, // breaks leading-summary contiguity
		{Role: provider.RoleUser, Content: priorDigest},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	prov := &fakeProvider{reply: "Standing facts: db is orion_prod_42"}
	a := New(prov, tool.NewRegistry(), sess,
		Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Canonical retains the prior digest; projection has exactly one summary.
	var priorInCanonical bool
	for _, m := range sess.Snapshot() {
		if strings.Contains(m.Content, "orion_prod_42") {
			priorInCanonical = true
		}
	}
	if !priorInCanonical {
		t.Fatal("canonical lost prior digest")
	}
	proj := visibleContext(a)
	summaries := 0
	for _, m := range proj {
		if isCompactionSummary(m) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("projection summaries = %d, want 1 (rolling merge)", summaries)
	}
	if !strings.Contains(joinContents(proj), "orion_prod_42") {
		t.Fatalf("rolling summary lost prior fact: %+v", proj)
	}
	// Prior digest body was part of the fold sent to the summarizer.
	if len(prov.got) < 2 || !strings.Contains(prov.got[1].Content, "orion_prod_42") {
		t.Fatalf("prior digest not folded into summarizer input: %+v", prov.got)
	}
}

func TestCompactKeepsErrorMessages(t *testing.T) {
	prov := &fakeProvider{reply: "- normal work summarized"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "bash", Arguments: `{"cmd":"bad"}`}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "bash", Content: "error: command failed"},
		{Role: provider.RoleUser, Content: "continue"},
		{Role: provider.RoleAssistant, Content: "continued"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir(), KeepPolicy: KeepErrors}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	// Canonical unchanged.
	if sess.Messages[3].Content != "error: command failed" {
		t.Fatalf("canonical error tool result changed: %+v", sess.Messages[3])
	}
	proj := visibleContext(a)
	var keptErr bool
	for i, m := range proj {
		if m.Role == provider.RoleTool && m.Content == "error: command failed" {
			keptErr = true
			if i == 0 || proj[i-1].Role != provider.RoleAssistant || len(proj[i-1].ToolCalls) == 0 {
				t.Fatalf("kept error lost its assistant tool call: %+v", proj)
			}
		}
	}
	if !keptErr {
		t.Fatalf("error tool result not kept in projection: %+v", proj)
	}
	if strings.Contains(prov.got[1].Content, "error: command failed") {
		t.Fatalf("kept error was still folded into summary input:\n%s", prov.got[1].Content)
	}
}

func TestCompactKeepsUserMarkedMessages(t *testing.T) {
	prov := &fakeProvider{reply: "- unmarked work summarized"}
	marked := "[[keep]] exact requirement " + strings.Repeat("must stay verbatim ", 200)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleUser, Content: marked},
		{Role: provider.RoleAssistant, Content: "worked"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir(), KeepPolicy: KeepUserMarked}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	var keptCanonical, keptProj bool
	for _, m := range sess.Messages {
		if m.Content == marked {
			keptCanonical = true
			break
		}
	}
	for _, m := range visibleContext(a) {
		if m.Content == marked {
			keptProj = true
			break
		}
	}
	if !keptCanonical {
		t.Fatalf("marked message missing from canonical: %+v", sess.Messages)
	}
	if !keptProj {
		t.Fatalf("marked message not kept in projection: %+v", visibleContext(a))
	}
	if strings.Contains(prov.got[1].Content, "exact requirement") {
		t.Fatalf("marked message was still folded into summary input:\n%s", prov.got[1].Content)
	}
}

func TestRunCompactsAfterFinalAnswer(t *testing.T) {
	// A turn that ends with a final answer (no trailing tool batch) must still
	// compact when the context is over the trigger; otherwise a large context
	// carries into the next turn un-folded and overflows the model window.
	big := strings.Repeat("old work ", 200)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleAssistant, Content: big},
	}}
	a := New(&fakeProvider{reply: "done", promptTokens: 95}, tool.NewRegistry(), sess,
		Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.Run(context.Background(), "what's the status?"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !hasCompactionSummary(visibleContext(a)) {
		t.Fatalf("final-answer turn over the trigger did not install projection summary")
	}
	// Canonical rewrite version stays 0; projection carries the fold.
	if got := sess.RewriteVersion(); got != 0 {
		t.Fatalf("canonical rewrite version = %d, want 0", got)
	}
}

func TestCompactFoldsSingleLargeMessage(t *testing.T) {
	prov := &fakeProvider{reply: "- captured the large file contents"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: strings.Repeat("large output line\n", 500)},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	before := len(sess.Messages)

	if err := a.compact(context.Background(), "auto", "", false); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(sess.Messages) != before {
		t.Fatalf("canonical changed: %d -> %d", before, len(sess.Messages))
	}
	proj := visibleContext(a)
	if !hasCompactionSummary(proj) || !strings.Contains(joinContents(proj), "large file contents") {
		t.Fatalf("single large message was not summarized into projection: %+v", proj)
	}
	if len(prov.got) == 0 || !strings.Contains(prov.got[1].Content, "large output line") {
		t.Fatalf("summarizer did not receive the large message: %+v", prov.got)
	}
}
