package serve

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

// A rebuilt card knows a call failed because the host said so, not because the
// words start with "error:" — a tool's own output can start that way, and a
// refusal's wording is not its identity.
func TestHistoryCarriesTheHostsAccountOfAFailure(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "use_capability"}}},
		{
			Role:        provider.RoleTool,
			ToolCallID:  "c1",
			Name:        "use_capability",
			Content:     "nothing about this sentence says it is a refusal",
			ToolFailure: &provider.ToolFailure{RefusalCode: "goal.no_active_turn"},
		},
	}
	var result historyMessage
	for _, hm := range historyMessages(msgs) {
		if hm.Role == "tool" {
			result = hm
		}
	}
	if !result.ToolFailed {
		t.Fatal("the rebuild lost the fact that the call failed")
	}
	if result.ToolRefusalCode != "goal.no_active_turn" {
		t.Fatalf("refusal identity = %q, want goal.no_active_turn", result.ToolRefusalCode)
	}
}

// A result that succeeded says nothing, so a card cannot read absence as failure.
func TestHistoryLeavesASuccessUnmarked(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "error: this is grep output, not a refusal"},
	}
	for _, hm := range historyMessages(msgs) {
		if hm.Role == "tool" && (hm.ToolFailed || hm.ToolRefusalCode != "") {
			t.Fatalf("a successful result was marked failed from its words: %+v", hm)
		}
	}
}

// A steer is stored the way the turn sent it: the host's transient blocks first,
// then the steer prefix, then the person's words. Reopening the session has to
// show the words. While the recogniser missed a block, a reopened transcript
// showed the user saying the host's mid-turn instructions to themselves.
func TestHistoryShowsAStoredSteerAsWhatWasTyped(t *testing.T) {
	stored := agent.WorkspaceBlock(`D:\Somewhere`, "git") + "\n\n" +
		sessionstore.MidTurnSteerPrefix + "\n" + "最后再报一下今天的日期"
	out := historyMessages([]provider.Message{{Role: provider.RoleUser, Content: stored}})
	if len(out) != 1 {
		t.Fatalf("history = %d messages, want 1", len(out))
	}
	if out[0].Role != "user" || !out[0].Steer {
		t.Errorf("role = %q steer = %v, want the person's own line, marked as guidance", out[0].Role, out[0].Steer)
	}
	if out[0].Content != "最后再报一下今天的日期" {
		t.Errorf("content = %q, want the person's own words", out[0].Content)
	}
	if out[0].HostAuthored {
		t.Error("a line the person typed was attributed to the host")
	}
}

// The host queues its own guidance through the same prefix. Marking it as the
// person's would have them reading their own words for something they never
// said, which is the one place that mistake is unrecoverable.
func TestHistoryDoesNotCreditTheHostsOwnSteerToThePerson(t *testing.T) {
	stored := sessionstore.HostNoticePrefix + "\n" + "a background job finished"
	out := historyMessages([]provider.Message{{Role: provider.RoleUser, Content: stored}})
	if !out[0].HostAuthored {
		t.Errorf("host steer = %+v, want it attributed to the host", out[0])
	}
}

// A reopened transcript has no turn_started to read the model off, so the
// message carries it. Without this a session reread the next day attributed
// every reply to whatever the composer happens to be set to now.
func TestHistoryNamesTheModelThatWroteAReply(t *testing.T) {
	out := historyMessages([]provider.Message{
		{Role: provider.RoleAssistant, Content: "done", ModelRef: "yyds/claude-opus-4.8"},
	})
	if out[0].ModelRef != "yyds/claude-opus-4.8" {
		t.Fatalf("reply = %+v, want the model that wrote it", out[0])
	}
}

// It is local metadata: a provider request that carried it would change the
// bytes every prefix-cache hash is taken over.
func TestTheModelNameNeverReachesAProviderRequest(t *testing.T) {
	sent := provider.ModelMessages([]provider.Message{
		{Role: provider.RoleAssistant, Content: "done", ModelRef: "yyds/claude-opus-4.8"},
	})
	if sent[0].ModelRef != "" {
		t.Fatalf("provider request carried %q", sent[0].ModelRef)
	}
}
