package tui

import (
	"encoding/json"
	"fmt"
	"testing"

	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
)

// A rewritten call is only distinguishable if the record says which text the
// model wrote. The card draws Arguments, and the server sends the proxy's
// effective text there; the original is a separate fact. Before this test the
// terminal's copy of the record dropped that fact, so a reopened session drew
// the host's rewrite as the model's own and the original was unreachable.
func TestRestoredToolCallKeepsTheArgumentsTheProxyRewrote(t *testing.T) {
	const (
		original  = `{"arguments":{"path":"a"}}`
		effective = `{"path":"a"}`
	)
	// The exact JSON /history sends, decoded the way the client decodes it.
	payload := fmt.Sprintf(
		`[{"role":"assistant","msgIndex":0,"toolCalls":[{"id":"c1","name":"read_file","arguments":%q,"argumentNormalization":{"reason":%q,"original":%q,"effective":%q}}]}]`,
		effective, provider.ArgumentEnvelopeUnwrapped, original, effective)
	var msgs []HistoryMessage
	if err := json.Unmarshal([]byte(payload), &msgs); err != nil {
		t.Fatalf("decode /history: %v", err)
	}
	tr := &Transcript{}
	tr.Restore(msgs)
	call := tr.Items[0].Tool
	// This is what the restored card draws, and it is not the model's text.
	if call.Args != effective {
		t.Fatalf("restored args = %s, want the effective arguments the server sends", call.Args)
	}
	n := call.ArgumentNormalization
	if n == nil {
		t.Fatalf("the restored call kept %s with no evidence it was a rewrite of %s", call.Args, original)
	}
	if n.Reason != provider.ArgumentEnvelopeUnwrapped || n.Original != original || n.Effective != effective {
		t.Fatalf("normalization = %+v, want the model's %s and the proxy's %s", n, original, effective)
	}
}

// A call the proxy left alone must read exactly as it did: the arguments the
// model wrote, and no normalization to explain.
func TestRestoredToolCallWithoutARewriteIsUnchanged(t *testing.T) {
	const args = `{"command":"make"}`
	tr := &Transcript{}
	tr.Restore([]HistoryMessage{{Role: "assistant", ToolCalls: []HistoryToolCall{{ID: "c1", Name: "bash", Arguments: args}}}})
	call := tr.Items[0].Tool
	if call.Args != args || call.ArgumentNormalization != nil {
		t.Fatalf("restored call = %+v, want its own args and no normalization", call)
	}
}

// A live call is drawn from the frame, and this change must not alter that
// path: the frame's fields land on the card's tool unchanged.
func TestLiveToolCallIsUnchanged(t *testing.T) {
	n := &provider.ArgumentNormalization{
		Reason: provider.ArgumentEnvelopeUnwrapped, Original: `{"arguments":{"path":"a"}}`, Effective: `{"path":"a"}`,
	}
	live := fold(eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{
		ID: "c1", Name: "read_file", Args: n.Effective, ArgumentNormalization: n, ResolvedName: "read_file", CapabilityID: "tool:read_file",
	}})
	call := live.Items[0].Tool
	if call.Args != n.Effective || call.ArgumentNormalization != n || call.ResolvedName != "read_file" || call.CapabilityID != "tool:read_file" {
		t.Fatalf("live call = %+v, want the frame's fields unchanged", call)
	}
	plain := fold(eventwire.Event{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "c2", Name: "bash", Args: `{"command":"make"}`}})
	if got := plain.Items[0].Tool; got.Args != `{"command":"make"}` || got.ArgumentNormalization != nil {
		t.Fatalf("live call without a rewrite = %+v, want its own args", got)
	}
}
