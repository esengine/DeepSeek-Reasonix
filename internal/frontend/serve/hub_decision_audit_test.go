package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
)

// ATTACK: two panes mint decision ids from their own counters, so both hand out
// "1". The id alone is therefore not authority — the route carrying it is the
// other half of the identity. A request addressed to one pane must not answer
// the other's card, however identical the two ids look.
func TestDecisionIdentityCannotCrossRuntimeRoutes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	closeSharedCatalogsOnCleanup(t)
	writeLocalProviderConfig(t)
	h := NewHub(HubOptions{})
	t.Cleanup(h.Shutdown)

	pane := func() (*control.Controller, string) {
		reg := tool.NewRegistry()
		reg.Add(agent.NewAskTool())
		ex := agent.New(&askingProvider{}, reg, sessionstore.NewSession(""), agent.Options{MaxSteps: 4}, event.Discard)
		bc := NewBroadcaster()
		ctrl := control.New(control.Options{
			Runner: ex, Executor: ex, Sink: bc,
			Policy: permission.New("ask", nil, nil, nil), SessionDir: testenv.TempDir(t),
		})
		ctrl.EnableInteractiveApproval()
		rt, err := h.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
		if err != nil {
			t.Fatalf("adopt: %v", err)
		}
		go ctrl.RunTurn(context.Background(), "pick one")
		return ctrl, rt.ID
	}
	a, _ := pane()
	b, idB := pane()

	card := func(c *control.Controller) control.Decision {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			if d := c.Decisions(); len(d) == 1 {
				return d[0]
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatal("no decision appeared")
		return control.Decision{}
	}
	cardA, cardB := card(a), card(b)
	if cardA.ID != cardB.ID {
		t.Skipf("panes minted %q and %q; this attack needs colliding ids", cardA.ID, cardB.ID)
	}

	// A's id, addressed to B — which holds a card with that very id, so only the
	// route keeps the two apart.
	req := httptest.NewRequest(http.MethodPost, runtimePrefix+idB+"/answer",
		strings.NewReader(`{"id":"`+cardA.ID+`","answers":[{"QuestionID":"q1","Selected":["A"]}]}`))
	req.Header.Set("Content-Type", "application/json")
	operatorHandler(h).ServeHTTP(httptest.NewRecorder(), req)

	if got := a.Decisions(); len(got) != 1 || got[0].ID != cardA.ID {
		t.Fatalf("a request routed to B answered A's card: %+v", got)
	}
	a.Cancel()
	b.Cancel()
	_ = cardB
}
