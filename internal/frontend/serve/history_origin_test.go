package serve

import (
	"testing"

	"reasonix/internal/contract/provider"
)

func TestHistorySaysWhoAHostRunTurnWasFor(t *testing.T) {
	origin := &provider.Origin{Kind: "message", Agent: "a-0123456789abcdef", Exchange: "x-0123456789abcdef"}
	got := historyMessages([]provider.Message{
		{Role: provider.RoleUser, Content: "framed message", HostAuthored: true, Origin: origin},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Content: "typed by the person"},
	})
	if len(got) != 3 || got[0].Origin == nil || got[0].Origin.Kind != "message" || got[0].Origin.Agent != origin.Agent || got[0].Origin.Exchange != origin.Exchange {
		t.Fatalf("history = %+v", got)
	}
	if got[2].Origin != nil {
		t.Fatalf("a message the person typed carries an origin: %+v", got[2].Origin)
	}
}
