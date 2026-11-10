package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/sessionstore"
)

type heldRunner struct {
	started chan string
	release chan struct{}
}

func (r *heldRunner) Run(_ context.Context, input string) error {
	r.started <- input
	<-r.release
	return nil
}

func heldController(t *testing.T) (*Controller, *heldRunner) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	dir := testenv.TempDir(t)
	r := &heldRunner{started: make(chan string, 8), release: make(chan struct{}, 8)}
	c := New(Options{Runner: r, Sink: event.Discard, SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl")})
	t.Cleanup(func() {
		close(r.release)
		c.Close()
		c.autosaveWG.Wait()
	})
	return c, r
}

func startTurn(t *testing.T, c *Controller, r *heldRunner) {
	t.Helper()
	c.Submit("first")
	select {
	case <-r.started:
	case <-time.After(testenv.Budget(t)):
		t.Fatal("the first turn never started")
	}
}

// A prompt an extension queues is the host's message about the extension, never
// the user's own words: it is queued with the host's origin, so a gate that asks
// whether the user wrote something sees that it did not.
func TestAPromptAnExtensionQueuesIsHostAuthoredAndLabelledWithItsSource(t *testing.T) {
	c, r := heldController(t)
	startTurn(t, c, r)

	if err := c.QueueExtensionPrompt("mod-x", "run the tests"); err != nil {
		t.Fatalf("QueueExtensionPrompt: %v", err)
	}
	var item *sessioninbox.InboxItemMeta
	for _, it := range c.InboxSnapshot().Items {
		if strings.HasPrefix(it.Source, "extension:") {
			item = &it
		}
	}
	if item == nil {
		t.Fatalf("nothing queued: %+v", c.InboxSnapshot().Items)
	}
	if !item.Origin.IsHost() || item.Source != "extension:mod-x" {
		t.Fatalf("queued item origin=%q source=%q, want host / extension:mod-x", item.Origin, item.Source)
	}
	if !strings.Contains(item.Preview, "mod-x") || !strings.Contains(item.Preview, "run the tests") {
		t.Fatalf("the queue row %q does not say whose words these are", item.Preview)
	}
}

func TestAnExtensionMayQueueOnlyTheUsersCountPerTurn(t *testing.T) {
	c, r := heldController(t)
	startTurn(t, c, r)
	for i := range 3 {
		if err := c.QueueExtensionPrompt("mod-x", "p"+string(rune('a'+i))); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	if err := c.QueueExtensionPrompt("mod-x", "one too many"); err == nil {
		t.Fatal("a fourth prompt in one turn was queued")
	}

	path := filepath.Join(os.Getenv("REASONIX_HOME"), "config.toml")
	if err := os.WriteFile(path, []byte("[mods]\nsubmit_per_turn = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.QueueExtensionPrompt("mod-x", "now allowed"); err != nil {
		t.Fatalf("submit_per_turn = 0 still refused: %v", err)
	}
}

// One prompt is bounded by the user's byte ceiling, and a refused one does not
// spend the per-turn count.
func TestAnExtensionPromptOverTheByteCeilingIsRefusedAndSpendsNoCount(t *testing.T) {
	c, r := heldController(t)
	startTurn(t, c, r)
	long := strings.Repeat("x", config.DefaultModsSubmitBytes+1)
	for range 5 {
		if err := c.QueueExtensionPrompt("mod-x", long); !errors.Is(err, ErrExtensionSubmitTooLong) {
			t.Fatalf("an over-long prompt: %v", err)
		}
	}
	if err := c.QueueExtensionPrompt("mod-x", strings.Repeat("x", config.DefaultModsSubmitBytes)); err != nil {
		t.Fatalf("a prompt at the ceiling after five refusals: %v", err)
	}

	path := filepath.Join(os.Getenv("REASONIX_HOME"), "config.toml")
	if err := os.WriteFile(path, []byte("[mods]\nsubmit_bytes = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.QueueExtensionPrompt("mod-x", long); err != nil {
		t.Fatalf("submit_bytes = 0 still refused: %v", err)
	}
}

func TestAnExtensionReadsTheLastMessagesRedactedAndBounded(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	dir := testenv.TempDir(t)
	sess := sessionstore.NewSession("sys")
	for i := range 30 {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("m", i+1)})
	}
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "key is sk-live-0123456789abcdef0123456789abcdef"})
	c := New(Options{Executor: agent.New(nil, nil, sess, agent.Options{}, event.Discard), SessionDir: dir, SessionPath: filepath.Join(dir, "s.jsonl"), Label: "test"})
	t.Cleanup(func() { c.Close(); c.autosaveWG.Wait() })

	got, err := c.ReadExtensionSession(protocol.SessionReadParams{What: protocol.SessionReadMessages})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 20 || got.Omitted != 11 {
		t.Fatalf("default read = %d messages, %d omitted; want 20 and 11", len(got.Messages), got.Omitted)
	}
	if strings.Contains(got.Messages[19].Text, "0123456789abcdef0123456789abcdef") {
		t.Fatalf("a credential left the host: %q", got.Messages[19].Text)
	}
	big, err := c.ReadExtensionSession(protocol.SessionReadParams{What: protocol.SessionReadMessages, Limit: 5000})
	if err != nil || len(big.Messages) != 31 {
		t.Fatalf("a large limit = %d messages, %v; want them all under the default ceiling of 200", len(big.Messages), err)
	}
	cfg := filepath.Join(os.Getenv("REASONIX_HOME"), "config.toml")
	if err := os.WriteFile(cfg, []byte("[mods]\nmessages_max = 10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	capped, _ := c.ReadExtensionSession(protocol.SessionReadParams{What: protocol.SessionReadMessages, Limit: 5000})
	if len(capped.Messages) != 10 {
		t.Fatalf("messages_max = 10 gave %d messages", len(capped.Messages))
	}
}

func TestAnExtensionReadsTheLastUsageOrAnEmptyOneBeforeTheFirstTurn(t *testing.T) {
	c, _ := heldController(t)
	got, err := c.ReadExtensionSession(protocol.SessionReadParams{What: protocol.SessionReadUsage})
	if err != nil || got.Usage == nil || got.Usage.PromptTokens != 0 {
		t.Fatalf("usage before any turn = %+v, %v; want an empty record", got, err)
	}
}

// A prompt an extension queues opens a host turn whose hooks may queue another.
// Only the user's own words end the count, or the chain would never stop.
func TestOnlyTheUsersOwnTurnClearsTheCountOfPromptsExtensionsQueued(t *testing.T) {
	c, _ := heldController(t)
	for range 3 {
		if err := c.QueueExtensionPrompt("mod-x", "p"); err != nil {
			t.Fatal(err)
		}
	}
	c.extPrompts.userSpoke(true) // a host turn: the queued prompt's own
	if err := c.QueueExtensionPrompt("mod-x", "p"); err == nil {
		t.Fatal("a host turn cleared the count and let the chain continue")
	}
	c.extPrompts.userSpoke(false)
	if err := c.QueueExtensionPrompt("mod-x", "p"); err != nil {
		t.Fatalf("the user's own turn did not clear the count: %v", err)
	}
}
