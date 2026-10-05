package acp

import (
	"encoding/json"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/permission"
)

func TestPromptTurnReportsUsageUpdate(t *testing.T) {
	prov := &scriptedProvider{name: "fake", responses: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}}
	factory := &e2eFactory{
		prov:       prov,
		tool:       fakeTool{name: "peek", ro: true},
		policy:     permission.New("ask", nil, nil, nil),
		sessionDir: testenv.TempDir(t),
		window:     200_000,
	}
	client, stop := startServer(t, factory)
	defer stop()

	sid := openSession(t, client)
	notifs, resp := drainPrompt(t, client, client.callAsync("session/prompt", SessionPromptParams{
		SessionID: sid,
		Prompt:    []ContentBlock{{Type: "text", Text: "hello"}},
	}))
	if resp.Error != nil {
		t.Fatalf("session/prompt: %+v", resp.Error)
	}
	var got []usageUpdate
	for _, n := range notifs {
		if n.Method != "session/update" || updateKind(t, n) != "usage_update" {
			continue
		}
		var p struct {
			SessionID string      `json:"sessionId"`
			Update    usageUpdate `json:"update"`
		}
		if err := json.Unmarshal(n.Params, &p); err != nil {
			t.Fatalf("decode usage_update: %v", err)
		}
		if p.SessionID != sid {
			t.Fatalf("usage_update for session %q, want %q", p.SessionID, sid)
		}
		got = append(got, p.Update)
	}
	if len(got) != 1 {
		t.Fatalf("usage_update notifications = %d, want 1 per turn", len(got))
	}
	if got[0].Size != 200_000 || got[0].Used <= 0 || got[0].Used > got[0].Size {
		t.Fatalf("usage_update = %+v, want used in (0, 200000] and size 200000", got[0])
	}
}

func TestUsageUpdateForCarriesCostOnlyWhenPriced(t *testing.T) {
	if _, ok := usageUpdateFor(10, 0, ReasonixUsage{}); ok {
		t.Fatal("a session with no context window must not report a zero-sized gauge")
	}
	u, ok := usageUpdateFor(1200, 8000, ReasonixUsage{})
	if !ok || u.Used != 1200 || u.Size != 8000 || u.Cost != nil {
		t.Fatalf("unpriced update = %+v ok=%v", u, ok)
	}
	amount, currency := 0.045, "USD"
	u, _ = usageUpdateFor(1200, 8000, ReasonixUsage{EstimatedCost: &amount, Currency: &currency})
	if u.Cost == nil || u.Cost.Amount != 0.045 || u.Cost.Currency != "USD" {
		t.Fatalf("priced update cost = %+v", u.Cost)
	}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sessionUpdate":"usage_update","used":1200,"size":8000,"cost":{"amount":0.045,"currency":"USD"}}`
	if string(raw) != want {
		t.Fatalf("wire = %s, want %s", raw, want)
	}
}
