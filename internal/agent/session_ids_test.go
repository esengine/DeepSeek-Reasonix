package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func idFixtureMessages() []provider.Message {
	return []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "hi", CreatedAt: 1},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "ls", Arguments: `{}`}}},
		{Role: provider.RoleTool, Content: "main.go", ToolCallID: "call_1", Name: "ls", ToolExecution: &provider.ToolExecution{}},
		{Role: provider.RoleAssistant, Content: "done", ReasoningContent: "r"},
	}
}

func withIDs(msgs []provider.Message) []provider.Message {
	out := append([]provider.Message(nil), msgs...)
	for i := range out {
		out[i].ID = NewMessageID()
	}
	return out
}

func TestNewMessageIDShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 2000)
	prev := ""
	for range 2000 {
		id := NewMessageID()
		if len(id) != messageIDLen {
			t.Fatalf("len(%q) = %d, want %d", id, len(id), messageIDLen)
		}
		for _, r := range id {
			if !strings.ContainsRune(messageIDAlphabet, r) {
				t.Fatalf("id %q contains %q outside the alphabet", id, r)
			}
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
		if prev != "" && id[:10] < prev[:10] {
			t.Fatalf("time prefix went backwards: %q after %q", id, prev)
		}
		prev = id
	}
}

func TestLegacyMessageIDsAreDeterministicAndPositional(t *testing.T) {
	a := idFixtureMessages()
	b := idFixtureMessages()
	assignLegacyMessageIDs("/tmp/x/sess-1.jsonl", a)
	assignLegacyMessageIDs("/other/dir/sess-1.jsonl", b)
	for i := range a {
		if a[i].ID == "" || len(a[i].ID) != messageIDLen {
			t.Fatalf("message %d id %q malformed", i, a[i].ID)
		}
		if a[i].ID != b[i].ID {
			t.Fatalf("message %d: same branch id and bytes derived %q vs %q", i, a[i].ID, b[i].ID)
		}
		for j := range a[:i] {
			if a[j].ID == a[i].ID {
				t.Fatalf("messages %d and %d share id %q", j, i, a[i].ID)
			}
		}
	}
	c := idFixtureMessages()
	assignLegacyMessageIDs("/tmp/x/sess-2.jsonl", c)
	if c[0].ID == a[0].ID {
		t.Fatal("different branch ids must derive different message ids")
	}
	d := idFixtureMessages()
	d[1].Content = "changed"
	assignLegacyMessageIDs("/tmp/x/sess-1.jsonl", d)
	if d[0].ID != a[0].ID {
		t.Fatal("an edit after message 0 must not change message 0's id")
	}
	if d[2].ID == a[2].ID {
		t.Fatal("an edited prefix must change the ids that follow it")
	}
	e := idFixtureMessages()
	e[3].ID = "PRESET"
	assignLegacyMessageIDs("/tmp/x/sess-1.jsonl", e)
	if e[3].ID != "PRESET" || e[4].ID != a[4].ID {
		t.Fatalf("preset ids must be kept and must not perturb later ids: %q %q", e[3].ID, e[4].ID)
	}
}

func TestLoadSessionDerivesStableIDsForLegacyTranscripts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	s := &Session{Messages: idFixtureMessages()}
	if err := s.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	first, err := LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	second, err := LoadSession(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(first.Messages) != len(second.Messages) || len(first.Messages) == 0 {
		t.Fatalf("lengths %d vs %d", len(first.Messages), len(second.Messages))
	}
	for i := range first.Messages {
		if first.Messages[i].ID == "" || first.Messages[i].ID != second.Messages[i].ID {
			t.Fatalf("message %d id unstable across loads: %q vs %q", i, first.Messages[i].ID, second.Messages[i].ID)
		}
	}
	if got := first.LeafID(); got != first.Messages[len(first.Messages)-1].ID {
		t.Fatalf("LeafID = %q", got)
	}
	if idx := first.IndexOfID(first.Messages[2].ID); idx != 2 {
		t.Fatalf("IndexOfID = %d, want 2", idx)
	}
	if idx := first.IndexOfID("missing"); idx != -1 {
		t.Fatalf("IndexOfID(missing) = %d", idx)
	}
}

func TestMessageIDsStayOutOfTranscriptIdentity(t *testing.T) {
	plain := idFixtureMessages()
	tagged := withIDs(plain)
	dp, err := digestSessionMessages(plain)
	if err != nil {
		t.Fatal(err)
	}
	dt, err := digestSessionMessages(tagged)
	if err != nil {
		t.Fatal(err)
	}
	if dp != dt {
		t.Fatal("digestSessionMessages must ignore message ids")
	}
	if !messagesHavePrefix(tagged, plain) || !messagesHavePrefix(plain, tagged) {
		t.Fatal("prefix comparison must ignore message ids")
	}
	if providerVisibleFingerprint(plain) != providerVisibleFingerprint(tagged) {
		t.Fatal("providerVisibleFingerprint must ignore message ids")
	}
	if coveredPrefixHash(plain, 3) != coveredPrefixHash(tagged, 3) {
		t.Fatal("coveredPrefixHash must ignore message ids")
	}
	hp, ht := newSessionTranscriptHasher(), newSessionTranscriptHasher()
	hp.addAll(plain)
	ht.addAll(tagged)
	sp, _ := hp.sum()
	st, _ := ht.sum()
	if sp != st {
		t.Fatal("sessionTranscriptHasher must ignore message ids")
	}
}

func TestSessionMutatorsMintMissingIDs(t *testing.T) {
	s := NewSession("sys")
	if s.Messages[0].ID == "" {
		t.Fatal("NewSession must mint the system message id")
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "a"})
	s.AddBatch(provider.Message{Role: provider.RoleUser, Content: "b"}, provider.Message{Role: provider.RoleAssistant, Content: "c", ID: "KEEP"})
	if s.Messages[1].ID == "" || s.Messages[2].ID == "" {
		t.Fatal("Add/AddBatch must mint ids")
	}
	if s.Messages[3].ID != "KEEP" {
		t.Fatalf("AddBatch must keep a caller id, got %q", s.Messages[3].ID)
	}
	if s.LeafID() != "KEEP" {
		t.Fatalf("LeafID = %q", s.LeafID())
	}
	kept := s.Messages[1].ID
	s.Rewrite([]provider.Message{s.Messages[0], s.Messages[1], {Role: provider.RoleAssistant, Content: "summary"}}, "test")
	if s.Messages[1].ID != kept || s.Messages[2].ID == "" {
		t.Fatal("Rewrite must keep existing ids and mint new ones")
	}
	s.Replace([]provider.Message{{Role: provider.RoleUser, Content: "x"}})
	s.ReplaceLocalMetadata([]provider.Message{s.Messages[0], {Role: provider.RoleAssistant, Content: "y"}})
	for i, m := range s.Messages {
		if m.ID == "" {
			t.Fatalf("message %d has no id after Replace/ReplaceLocalMetadata", i)
		}
	}
	empty := NewSession("")
	if empty.LeafID() != "" || empty.IndexOfID("x") != -1 {
		t.Fatal("empty session must report no leaf")
	}
	empty.SetLeadingSystemPrompt("p")
	if empty.Messages[0].ID == "" {
		t.Fatal("SetLeadingSystemPrompt must mint the prepended system id")
	}
}
