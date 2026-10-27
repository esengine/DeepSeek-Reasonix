package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/state/sessionstore"
)

func TestHostReportCannotBecomeAUserAddressGrant(t *testing.T) {
	var a *Agent
	sess := sessionstore.NewSession("")
	a = New(testutil.NewMock("m", testutil.Turn{Text: "first"}, testutil.Turn{Text: "done"}), tool.NewRegistry(), sess, Options{}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnStarted {
			a.SteerHostNotice(ChildMessageNotice(ChildMessage{Ref: "child", Text: elsewhere}))
		}
	}))
	if err := a.Run(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(a.userAddresses(), elsewhere) {
		t.Fatal("report address became a user-supplied address")
	}
	for _, m := range sess.Snapshot() {
		if m.Role == provider.RoleUser && m.Content != "" {
			if strings.Contains(m.Content, sessionstore.HostNoticePrefix) && !m.HostAuthored {
				t.Fatal("host report lacks durable origin")
			}
		}
	}
}

func TestSelectedContextDoesNotGrantItsAddressesAfterReplay(t *testing.T) {
	for _, userText := range []string{"", "inspect " + typed} {
		t.Run(userText, func(t *testing.T) {
			var a *Agent
			sess := sessionstore.NewSession("")
			a = New(testutil.NewMock("m", testutil.Turn{Text: "first"}, testutil.Turn{Text: "done"}), tool.NewRegistry(), sess, Options{}, event.FuncSink(func(e event.Event) {
				if e.Kind == event.TurnStarted {
					a.SteerWithContext(userText+"\nSelected result: "+elsewhere, userText)
				}
			}))
			if err := a.Run(context.Background(), "inspect"); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(sess.Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			var messages []provider.Message
			if err := json.Unmarshal(data, &messages); err != nil {
				t.Fatal(err)
			}
			addresses := agentWith(messages...).userAddresses()
			if slices.Contains(addresses, elsewhere) || (userText != "" && !slices.Contains(addresses, typed)) {
				t.Fatalf("wrong address authority after replay: %v", addresses)
			}
		})
	}
}
