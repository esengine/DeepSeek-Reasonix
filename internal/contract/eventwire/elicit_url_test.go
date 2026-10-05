package eventwire

import (
	"testing"

	"reasonix/internal/contract/event"
)

func TestURLAskWireDeclaresPersistencePolicyAndNormalizedTarget(t *testing.T) {
	w := ToWire(event.Event{Kind: event.AskRequest, Ask: event.Ask{Origin: &event.AskOrigin{URL: "https://0x7f.1/"}}})
	if !w.NonPersistable || w.Ask.Origin.URLHost != "127.0.0.1" || !w.Ask.Origin.URLLocal {
		t.Fatalf("URL prompt policy/host: %+v, %+v", w, w.Ask.Origin)
	}
	for _, e := range []event.Event{{Kind: event.AskRequest, Ask: event.Ask{Origin: &event.AskOrigin{Kind: "mcp", Message: "Ordinary form"}}}, {Kind: event.Notice, Text: "Decision receipt"}} {
		if ToWire(e).NonPersistable {
			t.Fatal("ordinary form or receipt became non-persistable")
		}
	}
}
